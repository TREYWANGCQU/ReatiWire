// collaboration_tool_solution/team_collab/pkg/tsnet/arbiter_test.go
package tsnet

import (
	"testing"
)

func TestArbitrateMode(t *testing.T) {
	// 1. nil config -> Simulation
	mode, _ := ArbitrateMode(nil)
	if mode != ModeSimulation {
		t.Fatalf("expected Simulation for nil config, got %v", mode)
	}

	// 2. default placeholder local domain -> Simulation
	cfgMock := &AppConfig{
		Server: ServerConfig{
			ControlURL: "https://headscale.reati-wire.local:8018",
		},
		Client: ClientConfig{
			AuthKey: "hs_key_9f83a04bc61244e8bc1a4c49d8e3b5e1",
		},
	}
	mode, _ = ArbitrateMode(cfgMock)
	if mode != ModeSimulation {
		t.Fatalf("expected Simulation for local placeholder domain, got %v", mode)
	}

	// 3. unreachable host -> Simulation
	cfgUnreachable := &AppConfig{
		Server: ServerConfig{
			ControlURL: "https://192.0.2.1:8018", // TEST-NET-1 unroutable
		},
		Client: ClientConfig{
			AuthKey: "hs_key_real_key_format_1234567890abcdef",
		},
	}
	mode, reason := ArbitrateMode(cfgUnreachable)
	if mode != ModeSimulation {
		t.Fatalf("expected Simulation for unreachable network endpoint, got %v, reason: %s", mode, reason)
	}
}
