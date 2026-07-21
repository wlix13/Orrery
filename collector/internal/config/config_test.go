package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wlix13/orrery/collector/internal/topology"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "orrery.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// Carries a token: every config needs one, or auth.allow_anonymous.
const minimal = `
auth:
  tokens:
    - name: ops
      token: t0ken
fleets:
  - name: main
    ssh: { user: ops, key_file: /tmp/key }
    nodes:
      - id: labX00
        address: 203.0.113.7
        type: exit
        dial: direct
`

// Same fleet without the auth block, for the auth-rule cases.
var minimalNoAuth = strings.Replace(minimal, "auth:\n  tokens:\n    - name: ops\n      token: t0ken\n", "", 1)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimal))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Listen != "127.0.0.1:9800" || cfg.Poll.Interval.D().Seconds() != 60 {
		t.Errorf("defaults not applied: %+v", cfg)
	}

	f := cfg.Fleets[0]
	if f.XrayAPIPort != 10085 || f.Dial != DialSSH || f.Collect.Hub != CollectFull || f.Collect.Exit != CollectTraffic {
		t.Errorf("fleet defaults not applied: %+v", f)
	}
}

func TestTokenEnvExpansion(t *testing.T) {
	t.Setenv("ORRERY_TEST_TOKEN", "s3cret")

	cfg, err := Load(writeConfig(t, "auth:\n  tokens:\n    - name: ops\n      token: ${ORRERY_TEST_TOKEN}\n"+minimalNoAuth))
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Auth.Tokens) != 1 || cfg.Auth.Tokens[0].Token != "s3cret" {
		t.Errorf("tokens = %+v, want one expanded to s3cret", cfg.Auth.Tokens)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name, yaml, wantErr string
	}{
		{"no fleets", "auth:\n  tokens:\n    - name: o\n      token: t\nlisten: 127.0.0.1:1\n", "at least one fleet"},
		{"loopback without credentials", "listen: 127.0.0.1:9800\n" + minimalNoAuth, "auth needs at least one"},
		{"public listen without credentials", "listen: 0.0.0.0:9800\n" + minimalNoAuth, "auth needs at least one"},
		{"tokens and allow_anonymous", "auth:\n  allow_anonymous: true\n  tokens:\n    - name: o\n      token: t\n" + minimalNoAuth, "cannot be combined"},
		{"token without a name", "auth:\n  tokens:\n    - token: t\n" + minimalNoAuth, "name is required"},
		{"token scoped to an unknown fleet", "auth:\n  tokens:\n    - name: o\n      token: t\n      fleets: [nope]\n" + minimalNoAuth, `unknown fleet "nope"`},
		{"duplicate token value", "auth:\n  tokens:\n    - name: a\n      token: t\n    - name: b\n      token: t\n" + minimalNoAuth, "duplicate token value"},
		{"bad dial", strings.Replace(minimal, "dial: direct", "dial: carrier-pigeon", 1), "invalid dial"},
		{"type required without topology", strings.Replace(minimal, "        type: exit\n", "", 1), "type is required"},
		{"ssh needs user", strings.Replace(strings.Replace(minimal, "dial: direct", "dial: ssh", 1), "user: ops, ", "", 1), "ssh.user is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestAnonymousAndDashboardDefaults(t *testing.T) {
	anon, err := Load(writeConfig(t, "auth:\n  allow_anonymous: true\n"+minimalNoAuth))
	if err != nil {
		t.Fatalf("allow_anonymous should validate without a token: %v", err)
	}

	if !anon.Auth.AllowAnonymous || len(anon.Auth.Tokens) != 0 {
		t.Errorf("auth = %+v", anon.Auth)
	}

	// Unset must read as requested but not explicit.
	if !anon.DashboardRequested() || anon.DashboardExplicitlyEnabled() {
		t.Errorf("unset dashboard: requested=%v explicit=%v", anon.DashboardRequested(), anon.DashboardExplicitlyEnabled())
	}

	off, err := Load(writeConfig(t, "dashboard:\n  enabled: false\n"+minimal))
	if err != nil {
		t.Fatal(err)
	}

	if off.DashboardRequested() || off.DashboardExplicitlyEnabled() {
		t.Errorf("dashboard.enabled: false should be off and not explicit-on")
	}

	on, err := Load(writeConfig(t, "dashboard:\n  enabled: true\n"+minimal))
	if err != nil {
		t.Fatal(err)
	}

	if !on.DashboardRequested() || !on.DashboardExplicitlyEnabled() {
		t.Errorf("dashboard.enabled: true should be both requested and explicit")
	}
}

func TestResolveNodes(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
auth:
  tokens:
    - name: ops
      token: t0ken
fleets:
  - name: main
    topology: unused-here
    xray_api_port: 10090
    ssh: { user: ops, key_file: /tmp/key }
    nodes:
      - id: mskA00
        collect: off
      - id: labX00
        address: 203.0.113.7
        type: exit
        dial: direct
`))
	if err != nil {
		t.Fatal(err)
	}

	topo := []topology.Node{
		{ID: "mskA00", Hostname: "mskA00.perigee.example", Region: "msk", Type: "hub"},
		{ID: "nlA00", Hostname: "nlA00.aphelion.example", Region: "nl", Type: "exit"},
	}

	nodes, err := cfg.Fleets[0].ResolveNodes(topo)
	if err != nil {
		t.Fatal(err)
	}

	if len(nodes) != 3 {
		t.Fatalf("resolved %d nodes, want 3", len(nodes))
	}

	byID := map[string]ResolvedNode{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	if n := byID["mskA00"]; n.Collect != CollectOff || n.Address != "mskA00.perigee.example" || n.Port != 10090 || n.Dial != DialSSH {
		t.Errorf("mskA00 = %+v", n)
	}

	if n := byID["nlA00"]; n.Collect != CollectTraffic || n.Key() != "main/nlA00" {
		t.Errorf("nlA00 = %+v", n)
	}

	if n := byID["labX00"]; n.Collect != CollectTraffic || n.Dial != DialDirect || n.Hostname != "203.0.113.7" {
		t.Errorf("labX00 = %+v", n)
	}
}
