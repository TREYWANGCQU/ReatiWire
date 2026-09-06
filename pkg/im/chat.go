// collaboration_tool_solution/team_collab/pkg/im/chat.go
package im

import (
	"sync"
	"time"
)

// MessageType 消息类型
type MessageType string

const (
	TypeChat   MessageType = "CHAT"
	TypeAck    MessageType = "ACK"
	TypeNotice MessageType = "NOTICE"
)

// DirectMessage 点对点即时消息报文
type DirectMessage struct {
	ID           string      `json:"id"`
	SenderIP     string      `json:"sender_ip"`
	SenderName   string      `json:"sender_name"`
	TargetIP     string      `json:"target_ip"`
	Type         MessageType `json:"type"`
	Content      string      `json:"content"`
	Timestamp    time.Time   `json:"timestamp"`
	IsDirectP2P  bool        `json:"is_direct_p2p"` // true: 直连绿色徽标, false: DERP中继黄色徽标
	LatencyMs    int64       `json:"latency_ms"`    // 当前端到端通信时延
	Acknowledged bool        `json:"acknowledged"`
}

// PeerPresence 对端状态与信息
type PeerPresence struct {
	IP          string    `json:"ip"`
	Name        string    `json:"name"`
	Role        string    `json:"role"` // "dev" 研发工程师, "member" 普通成员, "server" 目标服务器
	IsOnline    bool      `json:"is_online"`
	IsDirectP2P bool      `json:"is_direct_p2p"`
	LatencyMs   int64     `json:"latency_ms"`
	LastSeen    time.Time `json:"last_seen"`
}

// ChatManager 即时通讯管理器
type ChatManager struct {
	mu       sync.RWMutex
	messages map[string][]*DirectMessage // peerIP -> messages
	peers    map[string]*PeerPresence
}

// NewChatManager 创建即时消息管理器
func NewChatManager() *ChatManager {
	return &ChatManager{
		messages: make(map[string][]*DirectMessage),
		peers:    make(map[string]*PeerPresence),
	}
}

// AppendMessage 添加并存储一条聊天记录
func (cm *ChatManager) AppendMessage(peerIP string, msg *DirectMessage) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.messages[peerIP] = append(cm.messages[peerIP], msg)
}

// GetMessages 获取与指定对端的聊天记录
func (cm *ChatManager) GetMessages(peerIP string) []*DirectMessage {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	list, ok := cm.messages[peerIP]
	if !ok {
		return []*DirectMessage{}
	}
	result := make([]*DirectMessage, len(list))
	copy(result, list)
	return result
}

// UpdatePeerStatus 更新节点状态与 P2P / DERP 徽标指标
func (cm *ChatManager) UpdatePeerStatus(presence *PeerPresence) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.peers[presence.IP] = presence
}

// GetAllPeers 获取全部团队成员与服务器拓扑
func (cm *ChatManager) GetAllPeers() []*PeerPresence {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	peers := make([]*PeerPresence, 0, len(cm.peers))
	for _, p := range cm.peers {
		cp := *p
		peers = append(peers, &cp)
	}
	return peers
}

// SyncPeers 全量同步对端节点列表 (生产模式由 Tailnet 拓扑驱动)
func (cm *ChatManager) SyncPeers(peers []*PeerPresence) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	newMap := make(map[string]*PeerPresence)
	for _, p := range peers {
		if p != nil && p.IP != "" {
			newMap[p.IP] = p
		}
	}
	cm.peers = newMap
}

// ResetToMockPeers 重置为预设离线仿真成员列表 (仿真模式演示基线)
func (cm *ChatManager) ResetToMockPeers() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.peers = make(map[string]*PeerPresence)
	cm.peers["100.64.0.2"] = &PeerPresence{
		IP:          "100.64.0.2",
		Name:        "张工 (后端架构)",
		Role:        "dev",
		IsOnline:    true,
		IsDirectP2P: true,
		LatencyMs:   5,
		LastSeen:    time.Now(),
	}
	cm.peers["100.64.0.3"] = &PeerPresence{
		IP:          "100.64.0.3",
		Name:        "李工 (前端/移动端)",
		Role:        "dev",
		IsOnline:    true,
		IsDirectP2P: false,
		LatencyMs:   21,
		LastSeen:    time.Now(),
	}
	cm.peers["100.64.0.4"] = &PeerPresence{
		IP:          "100.64.0.4",
		Name:        "王运营 (产品交付)",
		Role:        "member",
		IsOnline:    true,
		IsDirectP2P: true,
		LatencyMs:   8,
		LastSeen:    time.Now(),
	}
}

