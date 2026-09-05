// collaboration_tool_solution/team_collab/pkg/proxy/socks5.go
package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	Socks5Version = 0x05
	AuthNone      = 0x00
	CmdConnect    = 0x01

	AtypIPv4   = 0x01
	AtypDomain = 0x03
	AtypIPv6   = 0x04

	RepSuccess           = 0x00
	RepServerFailure     = 0x01
	RepConnectionNotAlwd = 0x02
	RepNetworkUnreach    = 0x03
	RepHostUnreach       = 0x04
	RepConnRefused       = 0x05
	RepTTLExpired        = 0x06
	RepCmdNotSupported   = 0x07
	RepAtypNotSupported  = 0x08
)

// Dialer 网络连接发起接口 (由 tsnet 或本地回环提供)
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// DefaultDialer 标准 Go net.Dialer 实现
type DefaultDialer struct {
	Timeout time.Duration
}

func (d *DefaultDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: d.Timeout,
	}
	return dialer.DialContext(ctx, network, addr)
}

// DevServerInfo 目标开发服务器信息
type DevServerInfo struct {
	Name        string `json:"name"`
	IP          string `json:"ip"`
	SSHPort     int    `json:"ssh_port"`
	DBPort      int    `json:"db_port"`
	Description string `json:"description"`
	Tag         string `json:"tag"`
}

// SOCKS5Server 本地用户态环回代理网关 (127.0.0.1:1055)
type SOCKS5Server struct {
	mu           sync.Mutex
	listenAddr   string
	listener     net.Listener
	dialer       Dialer
	isRunning    bool
	activeConns  int64
	bytesRx      int64
	bytesTx      int64
	quitChan     chan struct{}
}

// NewSOCKS5Server 创建本地 SOCKS5 代理实例
func NewSOCKS5Server(listenAddr string, dialer Dialer) *SOCKS5Server {
	if listenAddr == "" {
		listenAddr = "127.0.0.1:1055"
	}
	if dialer == nil {
		dialer = &DefaultDialer{Timeout: 10 * time.Second}
	}
	return &SOCKS5Server{
		listenAddr: listenAddr,
		dialer:     dialer,
		quitChan:   make(chan struct{}),
	}
}

// Start 启动代理监听服务
func (s *SOCKS5Server) Start() error {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return errors.New("SOCKS5 server is already running")
	}

	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to bind SOCKS5 on %s: %w", s.listenAddr, err)
	}

	s.listener = ln
	s.isRunning = true
	s.quitChan = make(chan struct{})
	s.mu.Unlock()

	go s.serve()
	return nil
}

// Stop 停止代理服务
func (s *SOCKS5Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isRunning {
		return nil
	}
	s.isRunning = false
	close(s.quitChan)
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// IsRunning 检查代理服务运行状态
func (s *SOCKS5Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isRunning
}

// GetStats 获取代理吞吐统计
func (s *SOCKS5Server) GetStats() (int64, int64, int64) {
	return atomic.LoadInt64(&s.activeConns),
		atomic.LoadInt64(&s.bytesRx),
		atomic.LoadInt64(&s.bytesTx)
}

func (s *SOCKS5Server) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quitChan:
				return
			default:
				continue
			}
		}

		atomic.AddInt64(&s.activeConns, 1)
		go func(c net.Conn) {
			defer func() {
				c.Close()
				atomic.AddInt64(&s.activeConns, -1)
			}()
			_ = s.handleConn(c)
		}(conn)
	}
}

func (s *SOCKS5Server) handleConn(client net.Conn) error {
	// 1. 协商版本与认证方式
	buf := make([]byte, 256)
	if _, err := io.ReadFull(client, buf[:2]); err != nil {
		return err
	}
	if buf[0] != Socks5Version {
		return errors.New("unsupported socks version")
	}

	numMethods := int(buf[1])
	methods := make([]byte, numMethods)
	if _, err := io.ReadFull(client, methods); err != nil {
		return err
	}

	// 响应客户端: 免密接入 (NO AUTHENTICATION REQUIRED)
	if _, err := client.Write([]byte{Socks5Version, AuthNone}); err != nil {
		return err
	}

	// 2. 解析客户端连接请求 (CMD & ATYP)
	if _, err := io.ReadFull(client, buf[:4]); err != nil {
		return err
	}

	ver, cmd, _, atyp := buf[0], buf[1], buf[2], buf[3]
	if ver != Socks5Version {
		return errors.New("invalid protocol version in request")
	}
	if cmd != CmdConnect {
		// 仅支持 CONNECT 命令，其余返回不支持
		s.sendReply(client, RepCmdNotSupported, "0.0.0.0:0")
		return errors.New("unsupported command")
	}

	var targetHost string
	switch atyp {
	case AtypIPv4:
		if _, err := io.ReadFull(client, buf[:4]); err != nil {
			return err
		}
		targetHost = net.IP(buf[:4]).String()
	case AtypDomain:
		if _, err := io.ReadFull(client, buf[:1]); err != nil {
			return err
		}
		domainLen := int(buf[0])
		if _, err := io.ReadFull(client, buf[:domainLen]); err != nil {
			return err
		}
		targetHost = string(buf[:domainLen])
	case AtypIPv6:
		if _, err := io.ReadFull(client, buf[:16]); err != nil {
			return err
		}
		targetHost = net.IP(buf[:16]).String()
	default:
		s.sendReply(client, RepAtypNotSupported, "0.0.0.0:0")
		return errors.New("unsupported address type")
	}

	// 读取端口 (2 字节大端序)
	if _, err := io.ReadFull(client, buf[:2]); err != nil {
		return err
	}
	targetPort := binary.BigEndian.Uint16(buf[:2])
	targetAddr := net.JoinHostPort(targetHost, strconv.Itoa(int(targetPort)))

	// 3. 核心桥接: 调用 tsnet / WireGuard 驱动发起目标直连
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	destConn, err := s.dialer.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		s.sendReply(client, RepHostUnreach, "0.0.0.0:0")
		return fmt.Errorf("failed to dial target via tsnet: %w", err)
	}
	defer destConn.Close()

	// 成功通知客户端已连通
	if err := s.sendReply(client, RepSuccess, destConn.LocalAddr().String()); err != nil {
		return err
	}

	// 4. 双向无损数据通道复制 (零内存虚拟网卡桥接)
	errChan := make(chan error, 2)
	go func() {
		n, err := io.Copy(destConn, client)
		atomic.AddInt64(&s.bytesTx, n)
		errChan <- err
	}()
	go func() {
		n, err := io.Copy(client, destConn)
		atomic.AddInt64(&s.bytesRx, n)
		errChan <- err
	}()

	<-errChan
	return nil
}

func (s *SOCKS5Server) sendReply(client net.Conn, rep byte, bindAddr string) error {
	host, portStr, err := net.SplitHostPort(bindAddr)
	if err != nil {
		host = "127.0.0.1"
		portStr = "0"
	}
	port, _ := strconv.Atoi(portStr)

	ip := net.ParseIP(host).To4()
	if ip == nil {
		ip = []byte{127, 0, 0, 1}
	}

	reply := []byte{Socks5Version, rep, 0x00, AtypIPv4}
	reply = append(reply, ip...)

	portBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(portBuf, uint16(port))
	reply = append(reply, portBuf...)

	_, err = client.Write(reply)
	return err
}

// FormatSSHConfig 生成标准开发工具链 SSH ProxyCommand 片段
func FormatSSHConfig(hostAlias, targetIP, user string, proxyPort int) string {
	if proxyPort <= 0 {
		proxyPort = 1055
	}
	if user == "" {
		user = "root"
	}
	return fmt.Sprintf(`# [ReatiWire] 开发者工具链 SSH 配置 (免提权 P2P 直连开发机)
# 请将以下内容追加至本地 ~/.ssh/config 即可在终端或 VS Code Remote 中直连:
Host %s
    HostName %s
    User %s
    # Linux / macOS OpenSSH 原生 nc 支持:
    ProxyCommand nc -X 5 -x 127.0.0.1:%d %%h %%p
    # Windows 原生 OpenSSH 备用支持 (无需第三方工具):
    # ProxyCommand connect -S 127.0.0.1:%d %%h %%p
    StrictHostKeyChecking no
    ServerAliveInterval 15
`, hostAlias, targetIP, user, proxyPort, proxyPort)
}
