package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zxc0zxc0zxc/reconciliation/recon"
)

const valid = `
sources:
  ledger:
    address: localhost:9101
  provider:
    address: localhost:9102
    timeout: 5s
    page_size: 100

rules:
  - name: ledger-vs-provider
    sides: [ledger, provider]
    mode: item
    amount_tolerance: 50
    in_flight_window: 10m
    require_status_match: true
    thresholds:
      medium: 1000
      high: 100000
      critical: 10000000
`

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reconciliation.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	cfg, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.RuleNames(); len(got) != 1 || got[0] != "ledger-vs-provider" {
		t.Fatalf("rule names = %v", got)
	}

	rule, endpoints, err := cfg.Lookup("ledger-vs-provider")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if rule.Mode != recon.ModeItem || rule.AmountTolerance != 50 || rule.InFlightWindow != 10*time.Minute {
		t.Errorf("rule = %+v", rule)
	}
	if !rule.RequireStatusMatch {
		t.Error("require_status_match was not carried over")
	}
	if endpoints[0].Timeout != DefaultTimeout || endpoints[0].PageSize != DefaultPageSize {
		t.Errorf("defaults not applied: %+v", endpoints[0])
	}
	if endpoints[1].Timeout != 5*time.Second || endpoints[1].PageSize != 100 {
		t.Errorf("explicit values not carried over: %+v", endpoints[1])
	}
	if endpoints[0].Name != "ledger" || endpoints[1].Name != "provider" {
		t.Errorf("endpoints out of order: %+v", endpoints)
	}
}

func TestLookupUnknownRule(t *testing.T) {
	cfg, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, _, err := cfg.Lookup("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup error = %v, want ErrNotFound", err)
	}
}

func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "no sources", body: "rules:\n  - name: r\n    sides: [a, b]\n    mode: item\n"},
		{name: "no rules", body: "sources:\n  ledger:\n    address: localhost:1\n"},
		{
			name: "rule points at an undefined source",
			body: "sources:\n  ledger:\n    address: localhost:1\n" +
				"rules:\n  - name: r\n    sides: [ledger, ghost]\n    mode: item\n",
		},
		{
			name: "unknown field",
			body: "sources:\n  ledger:\n    address: localhost:1\n    retries: 3\n" +
				"rules:\n  - name: r\n    sides: [ledger, ledger]\n    mode: item\n",
		},
		{
			name: "duplicate rule name",
			body: "sources:\n  a:\n    address: localhost:1\n  b:\n    address: localhost:2\n" +
				"rules:\n  - name: r\n    sides: [a, b]\n    mode: item\n" +
				"  - name: r\n    sides: [b, a]\n    mode: item\n",
		},
		{
			name: "malformed duration",
			body: "sources:\n  a:\n    address: localhost:1\n  b:\n    address: localhost:2\n" +
				"rules:\n  - name: r\n    sides: [a, b]\n    mode: item\n    in_flight_window: soon\n",
		},
		{
			name: "one-sided rule",
			body: "sources:\n  a:\n    address: localhost:1\n" +
				"rules:\n  - name: r\n    sides: [a]\n    mode: item\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(write(t, tt.body)); err == nil {
				t.Fatal("Load() = nil error, want error")
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load error = %v, want os.ErrNotExist", err)
	}
}
