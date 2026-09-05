// collaboration_tool_solution/team_collab/pkg/circuitbreaker/breaker_test.go
package circuitbreaker

import (
	"context"
	"testing"
	"time"
)

func TestFastPathFailure(t *testing.T) {
	cb := NewCircuitBreaker(&BreakerConfig{
		TimeoutThreshold: 3000 * time.Millisecond,
		ProbeInterval:    10 * time.Second,
	})

	localEp := EndpointInfo{
		IP:          "100.64.0.5",
		HasIPv6:     false,
		NATTopology: NATSymmetric,
	}
	remoteEp := EndpointInfo{
		IP:          "100.64.0.2",
		HasIPv6:     false,
		NATTopology: NATSymmetric,
	}

	start := time.Now()
	status, err := cb.EvaluateAndDial(context.Background(), localEp, remoteEp, nil, nil)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.CurrentMode != ModeRelay {
		t.Errorf("expected ModeRelay, got %s", status.CurrentMode)
	}
	// 快速失败应在极短时间 (远小于 100ms) 内返回，无需等待 3000ms
	if duration > 100*time.Millisecond {
		t.Errorf("fast path took too long: %v", duration)
	}
}

func TestTimeoutFallback(t *testing.T) {
	cb := NewCircuitBreaker(&BreakerConfig{
		TimeoutThreshold: 200 * time.Millisecond, // 单元测试缩短阈值
		ProbeInterval:    10 * time.Second,
	})

	localEp := EndpointInfo{
		IP:          "100.64.0.5",
		HasIPv6:     true,
		NATTopology: NATCone,
	}
	remoteEp := EndpointInfo{
		IP:          "100.64.0.3",
		HasIPv6:     true,
		NATTopology: NATCone,
	}

	// 模拟阻塞无法连通的打洞
	hangingDialer := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}

	start := time.Now()
	status, err := cb.EvaluateAndDial(context.Background(), localEp, remoteEp, hangingDialer, nil)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.CurrentMode != ModeRelay {
		t.Errorf("expected fallback to ModeRelay, got %s", status.CurrentMode)
	}
	if duration < 180*time.Millisecond {
		t.Errorf("timeout triggered prematurely: %v", duration)
	}
}
