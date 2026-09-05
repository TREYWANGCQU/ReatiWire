// collaboration_tool_solution/team_collab/app.go
package main

import (
	"context"
	"fmt"
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
	NodeStatus       tsnet.NodeStatus `json:"node_status"`
	ProxyRunning     bool             `json:"proxy_running"`
	ProxyListenAddr  string           `json:"proxy_listen_addr"`
	ActiveConns      int64            `json:"active_conns"`
	BytesRx          int64            `json:"bytes_rx"`
	BytesTx          int64            `json:"bytes_tx"`
	HardTimeoutLimit int              `json:"hard_timeout_limit"` // 3000ms
	EgressLimitMbps  int              `json:"egress_limit_mbps"`  // 85 Mbps
}

// App Wails 宿主桥接与状态机控制器
type App struct {
	ctx            context.Context
	nodeMgr        *tsnet.NodeManager
	breaker        *circuitbreaker.CircuitBreaker
	proxySvr       *proxy.SOCKS5Server
	transferEngine *transfer.TransferEngine
	chatMgr        *im.ChatManager
	livekitGen     *livekit.TokenGenerator
	devServers     []proxy.DevServerInfo
}

// NewApp 创建应用程序实例
func NewApp() *App {
	nodeCfg := tsnet.NodeConfig{
		Hostname:   "reati-client-dev",
		ControlURL: "https://headscale.reati-wire.local:8018",
		AuthKey:    "preauth-reati-wire-key-sample",
	}
	nodeMgr := tsnet.NewNodeManager(nodeCfg)

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

	// 预设对端成员节点
	chatMgr.UpdatePeerStatus(&im.PeerPresence{
		IP:          "100.64.0.2",
		Name:        "张工 (后端架构)",
		Role:        "dev",
		IsOnline:    true,
		IsDirectP2P: true,
		LatencyMs:   5,
		LastSeen:    time.Now(),
	})
	chatMgr.UpdatePeerStatus(&im.PeerPresence{
		IP:          "100.64.0.3",
		Name:        "李工 (前端/移动端)",
		Role:        "dev",
		IsOnline:    true,
		IsDirectP2P: false, // 跨运营商 NAT -> 走 DERP 中继
		LatencyMs:   21,
		LastSeen:    time.Now(),
	})
	chatMgr.UpdatePeerStatus(&im.PeerPresence{
		IP:          "100.64.0.4",
		Name:        "王运营 (产品交付)",
		Role:        "member",
		IsOnline:    true,
		IsDirectP2P: true,
		LatencyMs:   8,
		LastSeen:    time.Now(),
	})

	return &App{
		nodeMgr:        nodeMgr,
		breaker:        breaker,
		proxySvr:       proxySvr,
		transferEngine: engine,
		chatMgr:        chatMgr,
		livekitGen:     livekitGen,
		devServers:     devServers,
	}
}

// Startup 在 Wails 客户端启动时执行生命周期初始化
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	// 1. 启动用户态 WireGuard 协议栈
	_ = a.nodeMgr.Start(ctx)
	// 2. 默认拉起本地 SOCKS5 代理网关 (127.0.0.1:1055)
	_ = a.proxySvr.Start()
}

// Shutdown 在退出时释放网络资源与监听
func (a *App) Shutdown(ctx context.Context) {
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
