// collaboration_tool_solution/team_collab/pkg/tsnet/interfaces.go
package tsnet

import (
	"context"
	"net"
	"time"
)

// RuntimeMode 系统运行模式
type RuntimeMode string

const (
	// ModeSimulation 离线仿真演示模式 (高保真交互原型)
	ModeSimulation RuntimeMode = "SIMULATION"
	// ModeProduction 生产真实联机模式 (真实 tsnet 协议栈与 Tailnet 拓扑)
	ModeProduction RuntimeMode = "PRODUCTION"
)

// PeerInfo 抽象对端节点信息
type PeerInfo struct {
	IP          string    `json:"ip"`
	Hostname    string    `json:"hostname"`
	Role        string    `json:"role"`
	IsOnline    bool      `json:"is_online"`
	IsDirectP2P bool      `json:"is_direct_p2p"`
	LatencyMs   int64     `json:"latency_ms"`
	LastSeen    time.Time `json:"last_seen"`
}

// INodeManager 用户态网络节点生命周期抽象接口
type INodeManager interface {
	Start(ctx context.Context) error
	Stop() error
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
	Listen(network, addr string) (net.Listener, error)
	GetStatus() NodeStatus
	GetMode() RuntimeMode
	GetPeers(ctx context.Context) ([]PeerInfo, error)
}
