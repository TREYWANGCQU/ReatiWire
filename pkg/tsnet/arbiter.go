// collaboration_tool_solution/team_collab/pkg/tsnet/arbiter.go
package tsnet

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ServerConfig 服务端配置
type ServerConfig struct {
	ControlURL string `json:"control_url"`
	LivekitURL string `json:"livekit_url"`
}

// ClientConfig 客户端配置
type ClientConfig struct {
	Hostname     string `json:"hostname"`
	AuthKey      string `json:"auth_key"`
	StateDir     string `json:"state_dir"`
	Ephemeral    bool   `json:"ephemeral"`
	SOCKS5Listen string `json:"socks5_listen"`
	WebListen    string `json:"web_listen"`
}

// AppConfig 客户端装载的顶层配置定义
type AppConfig struct {
	Mode   string       `json:"mode,omitempty"` // 可选: "auto" (默认), "production", "simulation"
	Server ServerConfig `json:"server"`
	Client ClientConfig `json:"client"`
}

// LoadAppConfig 从候选路径依次尝试加载 config.json
func LoadAppConfig(searchPaths ...string) (*AppConfig, string, error) {
	candidates := append(searchPaths,
		"config.json",
		filepath.Join(".", "config.json"),
		filepath.Join(".", "frontend", "config.json"),
		filepath.Join(".", "frontend", "public", "config.json"),
	)

	for _, p := range candidates {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err == nil {
			var cfg AppConfig
			if err := json.Unmarshal(data, &cfg); err == nil {
				return &cfg, p, nil
			}
		}
	}

	return nil, "", fmt.Errorf("no valid config.json found in candidate paths")
}

// ArbitrateMode 依据双轨规则评估当前应运行的模式
func ArbitrateMode(cfg *AppConfig) (RuntimeMode, string) {
	if cfg == nil {
		return ModeSimulation, "未检测到 config.json，保持高保真交互原型与离线仿真示例"
	}

	// 1. 显式配置覆盖检查
	modeLower := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if modeLower == "simulation" || modeLower == "mock" {
		return ModeSimulation, "配置显式指定 simulation 模式，保持离线仿真示例"
	}

	// 2. L1: 检查控制端 URL
	ctrlURL := strings.TrimSpace(cfg.Server.ControlURL)
	if ctrlURL == "" {
		return ModeSimulation, "未配置 Headscale 控制端地址 (control_url)，保持离线仿真示例"
	}
	if strings.Contains(ctrlURL, "reati-wire.local") || strings.Contains(ctrlURL, "sample") || strings.Contains(ctrlURL, "example") {
		return ModeSimulation, fmt.Sprintf("检测到示例占位控制端地址 (%s)，保持离线仿真示例", ctrlURL)
	}

	// 3. L1: 检查预授权密钥 AuthKey
	authKey := strings.TrimSpace(cfg.Client.AuthKey)
	if authKey == "" {
		return ModeSimulation, "未配置预授权密钥 (auth_key)，保持离线仿真示例"
	}
	// 默认演示占位密钥检查
	if strings.Contains(authKey, "sample") || authKey == "hs_key_9f83a04bc61244e8bc1a4c49d8e3b5e1" {
		if modeLower != "production" {
			return ModeSimulation, "检测到预设演示占位密钥，保持离线仿真示例"
		}
	}

	// 4. L2: 网络连通性快速探测 (严格限制 1500ms 超时)
	u, err := url.Parse(ctrlURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ModeSimulation, fmt.Sprintf("控制端 URL 解析失败 (%v)，保持离线仿真示例", err)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Proxy:           nil, // 直连探测，避免本地环路代理劫持
		DialContext: (&net.Dialer{
			Timeout: 1500 * time.Millisecond,
		}).DialContext,
	}
	defer tr.CloseIdleConnections()

	client := &http.Client{
		Transport: tr,
		Timeout:   1500 * time.Millisecond,
	}

	req, err := http.NewRequest(http.MethodGet, ctrlURL, nil)
	if err != nil {
		return ModeSimulation, fmt.Sprintf("构建探测请求失败 (%v)，保持离线仿真示例", err)
	}
	req.Header.Set("User-Agent", "ReatiWire-Probe/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return ModeSimulation, fmt.Sprintf("控制端端点不可达 (%s: %v)，平滑回退至离线仿真示例", u.Host, err)
	}
	_ = resp.Body.Close()

	return ModeProduction, fmt.Sprintf("检测到真实可达的控制端 (%s) 与有效配置，已切换为生产联机状态", u.Host)
}

// CreateAdaptiveNodeManager 根据配置判定结果自适应创建 NodeManager 实例
func CreateAdaptiveNodeManager(cfg *AppConfig) (INodeManager, RuntimeMode, string) {
	mode, reason := ArbitrateMode(cfg)

	hostname := "reati-client-dev"
	ctrlURL := "https://headscale.reati-wire.local:8018"
	authKey := "sample-key"
	stateDir := filepath.Join(os.TempDir(), "reati_wire_tsnet")
	ephemeral := false

	if cfg != nil {
		if cfg.Client.Hostname != "" {
			hostname = cfg.Client.Hostname
		}
		if cfg.Server.ControlURL != "" {
			ctrlURL = cfg.Server.ControlURL
		}
		if cfg.Client.AuthKey != "" {
			authKey = cfg.Client.AuthKey
		}
		if cfg.Client.StateDir != "" {
			stateDir = cfg.Client.StateDir
		}
		ephemeral = cfg.Client.Ephemeral
	}

	nodeCfg := NodeConfig{
		Hostname:   hostname,
		ControlURL: ctrlURL,
		AuthKey:    authKey,
		StateDir:   stateDir,
		Ephemeral:  ephemeral,
	}

	if mode == ModeProduction {
		return NewRealTsnetNodeManager(nodeCfg), ModeProduction, reason
	}

	return NewNodeManager(nodeCfg), ModeSimulation, reason
}
