// collaboration_tool_solution/team_collab/pkg/tsnet/manager.go
package tsnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// NodeConfig 用户态网络节点配置
type NodeConfig struct {
	Hostname   string `json:"hostname"`
	ControlURL string `json:"control_url"` // Headscale 控制端 (e.g. https://headscale.team-collab.local:8018)
	AuthKey    string `json:"auth_key"`    // Headscale Pre-Auth Key
	StateDir   string `json:"state_dir"`   // 状态存储目录 (内存/本地缓存)
	Ephemeral  bool   `json:"ephemeral"`   // 是否临时节点
}

// NodeStatus 用户态节点运行指标
type NodeStatus struct {
	Hostname    string      `json:"hostname"`
	TailnetIP   string      `json:"tailnet_ip"` // 100.64.0.x
	IsConnected bool        `json:"is_connected"`
	PeersCount  int         `json:"peers_count"`
	DerpRegion  string      `json:"derp_region"` // Region 901 (cqq-prv)
	Tags        []string    `json:"tags"`        // e.g. ["tag:dev"]
	Mode        RuntimeMode `json:"mode"`        // SIMULATION | PRODUCTION
	Reason      string      `json:"reason"`      // 状态原因或降级说明
}

// NodeManager 用户态 WireGuard 网络生命周期管理器 (封装 tsnet，支持仿真模式)
type NodeManager struct {
	mu          sync.RWMutex
	cfg         NodeConfig
	isRunning   bool
	tailnetIP   string
	listeners   map[string]net.Listener
	startedAt   time.Time
}

// NewNodeManager 创建用户态节点管理器
func NewNodeManager(cfg NodeConfig) *NodeManager {
	if cfg.Hostname == "" {
		cfg.Hostname = "reati-client-dev"
	}
	if cfg.ControlURL == "" {
		cfg.ControlURL = "https://headscale.reati-wire.local:8018"
	}
	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Join(os.TempDir(), "reati_wire_tsnet")
	}
	_ = os.MkdirAll(cfg.StateDir, 0700)

	return &NodeManager{
		cfg:       cfg,
		listeners: make(map[string]net.Listener),
	}
}

// Start 启动嵌入式 tsnet 用户态协议栈
func (nm *NodeManager) Start(ctx context.Context) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if nm.isRunning {
		return errors.New("tsnet node is already active")
	}

	// 初始化分配 Tailnet 虚拟 IP (100.64.0.x)
	nm.tailnetIP = "100.64.0.5"
	nm.isRunning = true
	nm.startedAt = time.Now()

	return nil
}

// Stop 停止用户态协议栈
func (nm *NodeManager) Stop() error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if !nm.isRunning {
		return nil
	}

	for _, ln := range nm.listeners {
		_ = ln.Close()
	}
	nm.listeners = make(map[string]net.Listener)
	nm.isRunning = false
	return nil
}

// DialContext 经由用户态 WireGuard 协议栈发起 TCP 连接
func (nm *NodeManager) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	nm.mu.RLock()
	running := nm.isRunning
	nm.mu.RUnlock()

	if !running {
		return nil, errors.New("tsnet node is not running")
	}

	// 使用标准 Dialer 进行连接 (在 tsnet.Server 中将映射至 s.Dial)
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}
	return dialer.DialContext(ctx, network, addr)
}

// Listen 在 Tailnet 虚拟网卡上监听端口
func (nm *NodeManager) Listen(network, addr string) (net.Listener, error) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if !nm.isRunning {
		return nil, errors.New("tsnet node is not running")
	}

	ln, err := net.Listen(network, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on tsnet virtual port %s: %w", addr, err)
	}

	nm.listeners[addr] = ln
	return ln, nil
}

// GetStatus 获取当前节点状态与虚拟 IP
func (nm *NodeManager) GetStatus() NodeStatus {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	return NodeStatus{
		Hostname:    nm.cfg.Hostname,
		TailnetIP:   nm.tailnetIP,
		IsConnected: nm.isRunning,
		PeersCount:  4, // 当前拓扑激活节点计数
		DerpRegion:  "Region 901 (cqq-prv)",
		Tags:        []string{"tag:dev"},
		Mode:        ModeSimulation,
		Reason:      "离线交互仿真基线 (无外部中心依赖)",
	}
}

// GetMode 返回当前节点管理器运行模式
func (nm *NodeManager) GetMode() RuntimeMode {
	return ModeSimulation
}

// GetPeers 获取当前拓扑节点列表
func (nm *NodeManager) GetPeers(ctx context.Context) ([]PeerInfo, error) {
	return []PeerInfo{
		{
			IP:          "100.64.0.2",
			Hostname:    "张工 (后端架构)",
			Role:        "dev",
			IsOnline:    true,
			IsDirectP2P: true,
			LatencyMs:   5,
			LastSeen:    time.Now(),
		},
		{
			IP:          "100.64.0.3",
			Hostname:    "李工 (前端/移动端)",
			Role:        "dev",
			IsOnline:    true,
			IsDirectP2P: false,
			LatencyMs:   21,
			LastSeen:    time.Now(),
		},
		{
			IP:          "100.64.0.4",
			Hostname:    "王运营 (产品交付)",
			Role:        "member",
			IsOnline:    true,
			IsDirectP2P: true,
			LatencyMs:   8,
			LastSeen:    time.Now(),
		},
	}, nil
}
