// collaboration_tool_solution/team_collab/app.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"reati_wire/pkg/circuitbreaker"
	"reati_wire/pkg/im"
	"reati_wire/pkg/livekit"
	"reati_wire/pkg/proxy"
	"reati_wire/pkg/transfer"
	"reati_wire/pkg/tsnet"
)

// SystemOverview 系统全局运行指标与安全概览
type SystemOverview struct {
	NodeStatus       tsnet.NodeStatus  `json:"node_status"`
	ProxyRunning     bool              `json:"proxy_running"`
	ProxyListenAddr  string            `json:"proxy_listen_addr"`
	ActiveConns      int64             `json:"active_conns"`
	BytesRx          int64             `json:"bytes_rx"`
	BytesTx          int64             `json:"bytes_tx"`
	HardTimeoutLimit int               `json:"hard_timeout_limit"` // 3000ms
	EgressLimitMbps  int               `json:"egress_limit_mbps"`  // 85 Mbps
	RuntimeMode      tsnet.RuntimeMode `json:"runtime_mode"`
	ModeReason       string            `json:"mode_reason"`
}

// App Wails 宿主桥接与状态机控制器
type App struct {
	ctx            context.Context
	nodeMgr        tsnet.INodeManager
	breaker        *circuitbreaker.CircuitBreaker
	proxySvr       *proxy.SOCKS5Server
	transferEngine *transfer.TransferEngine
	chatMgr        *im.ChatManager
	livekitGen     *livekit.TokenGenerator
	devServers     []proxy.DevServerInfo
	appConfig      *tsnet.AppConfig
	configPath     string
	runtimeMode    tsnet.RuntimeMode
	modeReason     string
	cancelSync     context.CancelFunc
}

// NewApp 创建应用程序实例 (自适应双轨模式)
func NewApp() *App {
	cfg, cfgPath, _ := tsnet.LoadAppConfig()
	nodeMgr, mode, reason := tsnet.CreateAdaptiveNodeManager(cfg)

	breakerCfg := &circuitbreaker.BreakerConfig{
		TimeoutThreshold: 3000 * time.Millisecond,
		ProbeInterval:    10 * time.Second,
	}
	breaker := circuitbreaker.NewCircuitBreaker(breakerCfg)

	proxySvr := proxy.NewSOCKS5Server("127.0.0.1:1055", nodeMgr)

	storageDir := filepath.Join(os.TempDir(), "reati_wire_downloads")
	engine, _ := transfer.NewTransferEngine(storageDir)

	chatMgr := im.NewChatManager()

	livekitGen := livekit.NewTokenGenerator(
		"reati_wire_api_key",
		"reati_wire_api_secret_32bytes_min",
	)

	// 初始化团队内部受保护目标开发服务器资产 (100.64.0.10+)
	devServers := []proxy.DevServerInfo{
		{
			Name:        "dev-linux-primary",
			IP:          "100.64.0.10",
			SSHPort:     22,
			DBPort:      3306,
			Description: "核心研发测试服务器 (Ubuntu 22.04 LTS, Docker/K8s)",
			Tag:         "tag:server",
		},
		{
			Name:        "qa-database-instance",
			IP:          "100.64.0.11",
			SSHPort:     22,
			DBPort:      5432,
			Description: "测试集群 PostgreSQL / Redis 数据存储节点",
			Tag:         "tag:server",
		},
	}

	if mode == tsnet.ModeSimulation {
		// 仿真模式：预设离线演示成员节点
		chatMgr.ResetToMockPeers()
	}

	return &App{
		nodeMgr:        nodeMgr,
		breaker:        breaker,
		proxySvr:       proxySvr,
		transferEngine: engine,
		chatMgr:        chatMgr,
		livekitGen:     livekitGen,
		devServers:     devServers,
		appConfig:      cfg,
		configPath:     cfgPath,
		runtimeMode:    mode,
		modeReason:     reason,
	}
}

// Startup 在 Wails 客户端启动时执行生命周期初始化
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	// 1. 启动用户态 WireGuard 协议栈
	err := a.nodeMgr.Start(ctx)
	if err != nil && a.runtimeMode == tsnet.ModeProduction {
		// 生产协议栈握手异常，安全平滑降级至仿真模式
		a.runtimeMode = tsnet.ModeSimulation
		a.modeReason = fmt.Sprintf("生产协议栈启动握手失败 (%v)，已安全回退至仿真演示", err)
		fallbackMgr := tsnet.NewNodeManager(tsnet.NodeConfig{Hostname: "reati-client-dev"})
		_ = fallbackMgr.Start(ctx)
		a.nodeMgr = fallbackMgr
		a.chatMgr.ResetToMockPeers()
	}

	// 2. 默认拉起本地 SOCKS5 代理网关 (127.0.0.1:1055)
	_ = a.proxySvr.Start()

	// 3. 生产模式下启动后台 Tailnet 真实拓扑轮询
	if a.runtimeMode == tsnet.ModeProduction {
		syncCtx, cancel := context.WithCancel(context.Background())
		a.cancelSync = cancel
		go a.pollTailnetPeers(syncCtx)
	}
}

// pollTailnetPeers 周期性同步 Tailnet 真实成员在线状态
func (a *App) pollTailnetPeers(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// 首次立即同步一次
	a.syncPeersFromNode(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.syncPeersFromNode(ctx)
		}
	}
}

func (a *App) syncPeersFromNode(ctx context.Context) {
	peers, err := a.nodeMgr.GetPeers(ctx)
	if err != nil || len(peers) == 0 {
		return
	}

	var presences []*im.PeerPresence
	for _, p := range peers {
		presences = append(presences, &im.PeerPresence{
			IP:          p.IP,
			Name:        p.Hostname,
			Role:        p.Role,
			IsOnline:    p.IsOnline,
			IsDirectP2P: p.IsDirectP2P,
			LatencyMs:   p.LatencyMs,
			LastSeen:    p.LastSeen,
		})
	}
	a.chatMgr.SyncPeers(presences)
}

// Shutdown 在退出时释放网络资源与监听
func (a *App) Shutdown(ctx context.Context) {
	if a.cancelSync != nil {
		a.cancelSync()
	}
	_ = a.proxySvr.Stop()
	_ = a.nodeMgr.Stop()
}

// GetSystemOverview 汇总客户端系统状态
func (a *App) GetSystemOverview() SystemOverview {
	nodeStatus := a.nodeMgr.GetStatus()
	conns, rx, tx := a.proxySvr.GetStats()

	return SystemOverview{
		NodeStatus:       nodeStatus,
		ProxyRunning:     a.proxySvr.IsRunning(),
		ProxyListenAddr:  "127.0.0.1:1055",
		ActiveConns:      conns,
		BytesRx:          rx,
		BytesTx:          tx,
		HardTimeoutLimit: 3000,
		EgressLimitMbps:  85,
		RuntimeMode:      a.runtimeMode,
		ModeReason:       a.modeReason,
	}
}

// TriggerP2PProbe 发起 P2P 双轨竞速与 3000ms 硬超时熔断探测
func (a *App) TriggerP2PProbe(peerIP string, forceSymmetric bool) (*circuitbreaker.LinkStatus, error) {
	localEp := circuitbreaker.EndpointInfo{
		IP:          "100.64.0.5",
		HasIPv6:     !forceSymmetric,
		NATTopology: circuitbreaker.NATCone,
	}
	remoteEp := circuitbreaker.EndpointInfo{
		IP:          peerIP,
		HasIPv6:     !forceSymmetric,
		NATTopology: circuitbreaker.NATCone,
	}

	if forceSymmetric {
		localEp.NATTopology = circuitbreaker.NATSymmetric
		remoteEp.NATTopology = circuitbreaker.NATSymmetric
		localEp.HasIPv6 = false
		remoteEp.HasIPv6 = false
	}

	status, err := a.breaker.EvaluateAndDial(context.Background(), localEp, remoteEp, nil, nil)
	return status, err
}

// ToggleSOCKS5Proxy 开关本地 SOCKS5 代理网关
func (a *App) ToggleSOCKS5Proxy(enable bool) (map[string]interface{}, error) {
	var err error
	if enable {
		err = a.proxySvr.Start()
	} else {
		err = a.proxySvr.Stop()
	}
	running := a.proxySvr.IsRunning()
	return map[string]interface{}{
		"running": running,
		"address": "127.0.0.1:1055",
	}, err
}

// GetDevServers 获取受保护的目标开发服务器列表
func (a *App) GetDevServers() []proxy.DevServerInfo {
	return a.devServers
}

// CopySSHConfig 生成并返回一键配置的 SSH ProxyCommand 规则
func (a *App) CopySSHConfig(hostAlias, targetIP string) string {
	return proxy.FormatSSHConfig(hostAlias, targetIP, "root", 1055)
}

// SendChatMessage 发送 P2P 即时消息
func (a *App) SendChatMessage(targetIP, content string) *im.DirectMessage {
	// 查询链路状态
	status := a.breaker.GetStatus(targetIP)
	isDirect := true
	latency := int64(8)
	if status != nil && status.CurrentMode == circuitbreaker.ModeRelay {
		isDirect = false
		latency = 24
	}

	msg := &im.DirectMessage{
		ID:           fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		SenderIP:     "100.64.0.5",
		SenderName:   "当前用户 (本机)",
		TargetIP:     targetIP,
		Type:         im.TypeChat,
		Content:      content,
		Timestamp:    time.Now(),
		IsDirectP2P:  isDirect,
		LatencyMs:    latency,
		Acknowledged: true,
	}

	a.chatMgr.AppendMessage(targetIP, msg)
	return msg
}

// GetChatHistory 获取与指定节点的聊天记录
func (a *App) GetChatHistory(peerIP string) []*im.DirectMessage {
	return a.chatMgr.GetMessages(peerIP)
}

// GetAllPeers 获取全部联系人与在线状态
func (a *App) GetAllPeers() []*im.PeerPresence {
	return a.chatMgr.GetAllPeers()
}

// CreateMeetingToken 签发 LiveKit 在线会议入会凭据
func (a *App) CreateMeetingToken(roomName, displayName string) (string, error) {
	identity := fmt.Sprintf("user_%d", time.Now().Unix()%10000)
	return a.livekitGen.CreateJoinToken(roomName, identity, displayName, 4*time.Hour)
}

// CreateAPIMux 构建供 Wails 内存级 AssetServer 路由的 HTTP 处理器 (零外部 TCP 端口暴露)
func (a *App) CreateAPIMux() http.Handler {
	mux := http.NewServeMux()

	// 1. API: 获取系统全局状态
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a.GetSystemOverview())
	})

	// 1.1 API: 获取客户端只读配置与双轨模式诊断信息
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		type ConfigResponse struct {
			tsnet.AppConfig
			RuntimeMode tsnet.RuntimeMode `json:"runtime_mode"`
			ModeReason  string            `json:"mode_reason"`
			ConfigPath  string            `json:"config_path"`
		}

		resp := ConfigResponse{
			RuntimeMode: a.runtimeMode,
			ModeReason:  a.modeReason,
			ConfigPath:  a.configPath,
		}
		if a.appConfig != nil {
			resp.AppConfig = *a.appConfig
		} else {
			resp.AppConfig = tsnet.AppConfig{
				Mode: "simulation",
				Server: tsnet.ServerConfig{
					ControlURL: "https://headscale.reati-wire.local:8018",
					LivekitURL: "https://sfu.reati-wire.local:7880",
				},
				Client: tsnet.ClientConfig{
					Hostname:     "reati-dev-zhangsan",
					AuthKey:      "hs_key_9f83a04bc61244e8bc1a4c49d8e3b5e1",
					StateDir:     "./data/tsnet_state",
					Ephemeral:    false,
					SOCKS5Listen: "127.0.0.1:1055",
					WebListen:    "内置内存桥接 (零端口暴露)",
				},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// 2. API: 获取联系人列表
	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a.GetAllPeers())
	})

	// 3. API: 获取目标开发服务器列表
	mux.HandleFunc("/api/dev-servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a.GetDevServers())
	})

	// 4. API: 触发 P2P / DERP 竞速探测与熔断测试
	mux.HandleFunc("/api/probe", func(w http.ResponseWriter, r *http.Request) {
		peerIP := r.URL.Query().Get("peer_ip")
		forceSym := r.URL.Query().Get("force_symmetric") == "true"
		if peerIP == "" {
			peerIP = "100.64.0.2"
		}
		status, err := a.TriggerP2PProbe(peerIP, forceSym)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(status)
	})

	// 5. API: 开关本地 SOCKS5 代理网关 (127.0.0.1:1055)
	mux.HandleFunc("/api/proxy/toggle", func(w http.ResponseWriter, r *http.Request) {
		enable := r.URL.Query().Get("enable") == "true"
		res, err := a.ToggleSOCKS5Proxy(enable)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	})

	// 6. API: 获取 SSH ProxyCommand 配置
	mux.HandleFunc("/api/ssh-config", func(w http.ResponseWriter, r *http.Request) {
		host := r.URL.Query().Get("host")
		ip := r.URL.Query().Get("ip")
		if host == "" {
			host = "dev-linux-primary"
		}
		if ip == "" {
			ip = "100.64.0.10"
		}
		cfgText := a.CopySSHConfig(host, ip)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(cfgText))
	})

	// 7. API: 发送 P2P 聊天消息
	mux.HandleFunc("/api/chat/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			TargetIP string `json:"target_ip"`
			Content  string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		msg := a.SendChatMessage(req.TargetIP, req.Content)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msg)
	})

	// 8. API: 获取聊天记录
	mux.HandleFunc("/api/chat/history", func(w http.ResponseWriter, r *http.Request) {
		peerIP := r.URL.Query().Get("peer_ip")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a.GetChatHistory(peerIP))
	})

	// 9. API: 签发 LiveKit 会议 Token
	mux.HandleFunc("/api/meeting/token", func(w http.ResponseWriter, r *http.Request) {
		room := r.URL.Query().Get("room")
		name := r.URL.Query().Get("name")
		if room == "" {
			room = "general-collab-room"
		}
		if name == "" {
			name = "研发工程师"
		}
		token, err := a.CreateMeetingToken(room, name)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"room":  room,
			"token": token,
		})
	})

	return mux
}
