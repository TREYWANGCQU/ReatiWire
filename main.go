// collaboration_tool_solution/team_collab/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	fmt.Println("================================================================================")
	fmt.Println("  ReatiWire: 20人轻量私有 IM 与在线会议协同系统 (All-in-One Dedicated Client)")
	fmt.Println("================================================================================")

	app := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.Startup(ctx)
	defer app.Shutdown(ctx)

	// 提供内置 HTTP API 与前端静态资源服务 (支持浏览器直接访问与 Wails 混合驱动)
	mux := http.NewServeMux()

	// 1. API: 获取系统全局状态
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(app.GetSystemOverview())
	})

	// 1.1 API: 获取客户端只读配置 (供前端安全自查，api_key 与 api_secret 已全面物理移除)
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		configPath := filepath.Join(".", "config.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			configPath = filepath.Join(".", "frontend", "public", "config.json")
			data, err = os.ReadFile(configPath)
		}
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"server": map[string]string{
					"control_url": "https://headscale.reati-wire.local:8018",
					"livekit_url": "https://sfu.reati-wire.local:7880",
				},
				"client": map[string]interface{}{
					"hostname":      "reati-dev-zhangsan",
					"socks5_listen": "127.0.0.1:1055",
					"web_listen":    "127.0.0.1:34115",
					"state_dir":     "./data/tsnet_state",
					"ephemeral":     false,
				},
			})
			return
		}
		_, _ = w.Write(data)
	})

	// 2. API: 获取联系人列表
	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(app.GetAllPeers())
	})

	// 3. API: 获取目标开发服务器列表
	mux.HandleFunc("/api/dev-servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(app.GetDevServers())
	})

	// 4. API: 触发 P2P / DERP 竞速探测与熔断测试
	mux.HandleFunc("/api/probe", func(w http.ResponseWriter, r *http.Request) {
		peerIP := r.URL.Query().Get("peer_ip")
		forceSym := r.URL.Query().Get("force_symmetric") == "true"
		if peerIP == "" {
			peerIP = "100.64.0.2"
		}
		status, err := app.TriggerP2PProbe(peerIP, forceSym)
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
		res, err := app.ToggleSOCKS5Proxy(enable)
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
		cfgText := app.CopySSHConfig(host, ip)
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
		msg := app.SendChatMessage(req.TargetIP, req.Content)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msg)
	})

	// 8. API: 获取聊天记录
	mux.HandleFunc("/api/chat/history", func(w http.ResponseWriter, r *http.Request) {
		peerIP := r.URL.Query().Get("peer_ip")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(app.GetChatHistory(peerIP))
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
		token, err := app.CreateMeetingToken(room, name)
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

	// 10. 静态前端资源挂载
	frontendDir := filepath.Join(".", "frontend")
	if fi, err := os.Stat(frontendDir); err == nil && fi.IsDir() {
		mux.Handle("/", http.FileServer(http.Dir(frontendDir)))
	}

	serverPort := "34115"
	server := &http.Server{
		Addr:         "127.0.0.1:" + serverPort,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		fmt.Printf("[+] 客户端网络与调度核心就绪\n")
		fmt.Printf("[+] 用户态 WireGuard 虚拟 IP: 100.64.0.5 (无需操作系统管理员提权)\n")
		fmt.Printf("[+] 本地 SOCKS5 代理网关监听: 127.0.0.1:1055 (供 SSH / VS Code / DB 直连目标开发机)\n")
		fmt.Printf("[+] 客户端 Web 表现层已启动: http://127.0.0.1:%s\n", serverPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server listen failed: %v", err)
		}
	}()

	// 捕获终止信号优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n[*] 正在关闭协同客户端网络栈与代理监听...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	fmt.Println("[*] 退出完成")
}
