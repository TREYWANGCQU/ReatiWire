// collaboration_tool_solution/team_collab/pkg/proxy/socks5_test.go
package proxy

import (
	"strings"
	"testing"
)

func TestFormatSSHConfig(t *testing.T) {
	cfg := FormatSSHConfig("dev-server", "100.64.0.10", "root", 1055)
	if !strings.Contains(cfg, "Host dev-server") {
		t.Errorf("missing Host alias")
	}
	if !strings.Contains(cfg, "HostName 100.64.0.10") {
		t.Errorf("missing HostName IP")
	}
	if !strings.Contains(cfg, "ProxyCommand nc -X 5 -x 127.0.0.1:1055 %h %p") {
		t.Errorf("invalid ProxyCommand line: %s", cfg)
	}
}

func TestSOCKS5ServerLifecycle(t *testing.T) {
	server := NewSOCKS5Server("127.0.0.1:21055", nil)
	if server.IsRunning() {
		t.Errorf("server should not be running before Start")
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	if !server.IsRunning() {
		t.Errorf("server should be running after Start")
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}
	if server.IsRunning() {
		t.Errorf("server should not be running after Stop")
	}
}
