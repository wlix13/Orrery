// Package config loads and validates orrery.yaml.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Collect levels control how much data is pulled from a node.
const (
	CollectFull    = "full"    // tags + per-user traffic + online users + sys stats
	CollectTraffic = "traffic" // tags + sys stats
	CollectOff     = "off"     // node is registered but never polled
)

// Dial modes.
const (
	DialSSH    = "ssh"    // gRPC through an SSH tunnel to the node's loopback API
	DialDirect = "direct" // plain gRPC to <address>:<api port>
)

// SSH host-key verification modes.
const (
	VerifyKnownHosts = "known_hosts" // match against a known_hosts file
	VerifySSHFP      = "sshfp"       // match the key fingerprint against DNS SSHFP records
	VerifyInsecure   = "insecure"    // accept any host key (logs a warning)
)

// Node types (mirror HexRift region types).
const (
	TypeHub  = "hub"
	TypeExit = "exit"
)

// Duration wraps time.Duration with YAML string parsing ("60s", "72h").
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}

	*d = Duration(parsed)

	return nil
}

func (d Duration) D() time.Duration { return time.Duration(d) }

type Config struct {
	Dashboard DashboardConfig `yaml:"dashboard"`
	// SSHFPRequireDNSSEC rejects SSHFP records not DNSSEC-authenticated
	// (the resolver's AD bit). Defaults to true; pointer distinguishes unset.
	SSHFPRequireDNSSEC *bool  `yaml:"sshfp_require_dnssec"`
	Listen             string `yaml:"listen"`
	DB                 string `yaml:"db"`
	// HostKeyVerify is the global default SSH host-key verification mode
	// (known_hosts | sshfp | insecure); fleets may override it.
	HostKeyVerify string          `yaml:"host_key_verify"`
	Fleets        []FleetConfig   `yaml:"fleets"`
	Auth          AuthConfig      `yaml:"auth"`
	Poll          PollConfig      `yaml:"poll"`
	Retention     RetentionConfig `yaml:"retention"`
	Metrics       MetricsConfig   `yaml:"metrics"`
}

type AuthConfig struct {
	// Tokens are bearer credentials, each scoped to a set of fleets.
	Tokens []TokenConfig `yaml:"tokens"`
	// AllowAnonymous serves the API and /metrics with no authentication.
	AllowAnonymous bool `yaml:"allow_anonymous"`
}

// TokenConfig is one bearer credential. An empty Fleets list means every fleet.
type TokenConfig struct {
	Name   string   `yaml:"name"`
	Token  string   `yaml:"token"`
	Fleets []string `yaml:"fleets"`
}

type PollConfig struct {
	Interval Duration `yaml:"interval"`
	Timeout  Duration `yaml:"timeout"`
}

type RetentionConfig struct {
	Minute Duration `yaml:"minute"`
	Hour   Duration `yaml:"hour"`
}

type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
}

// DashboardConfig controls whether the embedded SPA is served at /.
// Pointer distinguishes unset from an explicit choice.
type DashboardConfig struct {
	Enabled *bool `yaml:"enabled"`
}

type FleetConfig struct {
	Collect     CollectConfig  `yaml:"collect"`
	Name        string         `yaml:"name"`
	Topology    string         `yaml:"topology"`
	Dial        string         `yaml:"dial"`
	Nodes       []NodeOverride `yaml:"nodes"`
	SSH         SSHConfig      `yaml:"ssh"`
	XrayAPIPort int            `yaml:"xray_api_port"`
}

type SSHConfig struct {
	User       string `yaml:"user"`
	KeyFile    string `yaml:"key_file"`
	KnownHosts string `yaml:"known_hosts"`
	Port       int    `yaml:"port"`
}

type CollectConfig struct {
	Hub  string `yaml:"hub"`
	Exit string `yaml:"exit"`
}

// NodeOverride tweaks or adds a node relative to the fleet's topology file.
type NodeOverride struct {
	ID          string `yaml:"id"`
	Address     string `yaml:"address"`
	Type        string `yaml:"type"` // required only for nodes absent from topology
	Region      string `yaml:"region"`
	Dial        string `yaml:"dial"`
	Collect     string `yaml:"collect"`
	XrayAPIPort int    `yaml:"xray_api_port"`
}

// Load reads, expands env vars in secrets, applies defaults, and validates.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	cfg.applyDefaults()

	for i := range cfg.Auth.Tokens {
		cfg.Auth.Tokens[i].Token = os.ExpandEnv(cfg.Auth.Tokens[i].Token)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:9800"
	}

	if c.DB == "" {
		c.DB = "orrery.db"
	}

	if c.Poll.Interval == 0 {
		c.Poll.Interval = Duration(60 * time.Second)
	}

	if c.Poll.Timeout == 0 {
		c.Poll.Timeout = Duration(15 * time.Second)
	}

	if c.Retention.Minute == 0 {
		c.Retention.Minute = Duration(72 * time.Hour)
	}

	if c.Retention.Hour == 0 {
		c.Retention.Hour = Duration(90 * 24 * time.Hour)
	}

	if c.HostKeyVerify == "" {
		c.HostKeyVerify = VerifyKnownHosts
	}

	for i := range c.Fleets {
		f := &c.Fleets[i]
		if f.XrayAPIPort == 0 {
			f.XrayAPIPort = 10085
		}

		if f.Dial == "" {
			f.Dial = DialSSH
		}

		if f.SSH.Port == 0 {
			f.SSH.Port = 22
		}

		if f.SSH.KnownHosts == "" {
			f.SSH.KnownHosts = expandHome("~/.ssh/known_hosts")
		} else {
			f.SSH.KnownHosts = expandHome(f.SSH.KnownHosts)
		}

		f.SSH.KeyFile = expandHome(f.SSH.KeyFile)
		if f.Collect.Hub == "" {
			f.Collect.Hub = CollectFull
		}

		if f.Collect.Exit == "" {
			f.Collect.Exit = CollectTraffic
		}
	}
}

func (c *Config) validate() error {
	var errs []error
	if len(c.Fleets) == 0 {
		errs = append(errs, errors.New("at least one fleet is required"))
	}

	errs = append(errs, c.validateAuth()...)

	if !validVerify(c.HostKeyVerify) {
		errs = append(errs, fmt.Errorf("host_key_verify must be known_hosts|sshfp|insecure"))
	}

	seen := map[string]bool{}

	for i := range c.Fleets {
		f := &c.Fleets[i]
		if f.Name == "" {
			errs = append(errs, fmt.Errorf("fleets[%d]: name is required", i))
			continue
		}

		if seen[f.Name] {
			errs = append(errs, fmt.Errorf("fleet %q: duplicate fleet name", f.Name))
		}

		seen[f.Name] = true

		errs = append(errs, f.validate()...)
	}

	return errors.Join(errs...)
}

// validateAuth checks the credential set. Not conditioned on the listen
// address: a tunnel can republish loopback.
func (c *Config) validateAuth() []error {
	var errs []error

	a := &c.Auth

	if len(a.Tokens) == 0 && !a.AllowAnonymous {
		errs = append(errs, errors.New(
			"auth needs at least one entry in auth.tokens (or auth.allow_anonymous: true to serve "+
				"the API and /metrics to anyone who can reach "+c.Listen+")"))
	}

	if len(a.Tokens) > 0 && a.AllowAnonymous {
		errs = append(errs, errors.New("auth.allow_anonymous cannot be combined with auth.tokens: they would never be checked"))
	}

	fleets := map[string]bool{}
	for i := range c.Fleets {
		fleets[c.Fleets[i].Name] = true
	}

	seenName := map[string]bool{}
	seenToken := map[string]bool{}

	for i, t := range a.Tokens {
		switch {
		case t.Name == "":
			errs = append(errs, fmt.Errorf("auth.tokens[%d]: name is required", i))
		case seenName[t.Name]:
			errs = append(errs, fmt.Errorf("auth.tokens[%d]: duplicate name %q", i, t.Name))
		}

		seenName[t.Name] = true

		if t.Token == "" {
			errs = append(errs, fmt.Errorf("auth.tokens[%d] (%s): token is empty", i, t.Name))
		} else if seenToken[t.Token] {
			errs = append(errs, fmt.Errorf("auth.tokens[%d] (%s): duplicate token value", i, t.Name))
		}

		seenToken[t.Token] = true
		errs = append(errs, unknownFleets(fmt.Sprintf("auth.tokens[%d] (%s)", i, t.Name), t.Fleets, fleets)...)
	}

	return errs
}

// unknownFleets reports scope entries that name no configured fleet, which is
// almost always a typo that would silently grant nothing.
func unknownFleets(where string, scope []string, fleets map[string]bool) []error {
	var errs []error

	for _, f := range scope {
		if !fleets[f] {
			errs = append(errs, fmt.Errorf("%s: unknown fleet %q", where, f))
		}
	}

	return errs
}

func (f *FleetConfig) validate() []error {
	var errs []error

	prefix := fmt.Sprintf("fleet %q", f.Name)

	if strings.Contains(f.Name, "/") {
		errs = append(errs, fmt.Errorf("%s: name must not contain '/'", prefix))
	}

	if !validDial(f.Dial) {
		errs = append(errs, fmt.Errorf("%s: dial must be %q or %q", prefix, DialSSH, DialDirect))
	}

	if !validCollect(f.Collect.Hub) || !validCollect(f.Collect.Exit) {
		errs = append(errs, fmt.Errorf("%s: collect levels must be full|traffic|off", prefix))
	}

	for _, n := range f.Nodes {
		errs = append(errs, f.validateNode(n, prefix)...)
	}

	if f.Topology == "" && len(f.Nodes) == 0 {
		errs = append(errs, fmt.Errorf("%s: needs a topology file or explicit nodes", prefix))
	}

	errs = append(errs, f.validateSSH(prefix)...)

	return errs
}

func (f *FleetConfig) validateNode(n NodeOverride, prefix string) []error {
	if n.ID == "" {
		return []error{fmt.Errorf("%s: node override without id", prefix)}
	}

	var errs []error

	if n.Dial != "" && !validDial(n.Dial) {
		errs = append(errs, fmt.Errorf("%s node %q: invalid dial %q", prefix, n.ID, n.Dial))
	}

	if n.Collect != "" && !validCollect(n.Collect) {
		errs = append(errs, fmt.Errorf("%s node %q: invalid collect %q", prefix, n.ID, n.Collect))
	}

	if n.Type != "" && n.Type != TypeHub && n.Type != TypeExit {
		errs = append(errs, fmt.Errorf("%s node %q: invalid type %q", prefix, n.ID, n.Type))
	}

	if f.Topology == "" && n.Type == "" {
		errs = append(errs, fmt.Errorf("%s node %q: type is required without a topology file", prefix, n.ID))
	}

	return errs
}

// usesSSH reports whether any node in the fleet would dial over SSH:
// topology nodes inherit the fleet dial; explicit nodes may override it.
func (f *FleetConfig) usesSSH() bool {
	if f.Topology != "" && f.Dial == DialSSH {
		return true
	}

	for _, n := range f.Nodes {
		effDial := n.Dial
		if effDial == "" {
			effDial = f.Dial
		}

		if effDial == DialSSH {
			return true
		}
	}

	return false
}

func (f *FleetConfig) validateSSH(prefix string) []error {
	if !f.usesSSH() {
		return nil
	}

	var errs []error

	if f.SSH.User == "" {
		errs = append(errs, fmt.Errorf("%s: ssh.user is required for dial: ssh", prefix))
	}

	if f.SSH.KeyFile == "" {
		errs = append(errs, fmt.Errorf("%s: ssh.key_file is required for dial: ssh", prefix))
	}

	return errs
}

// RequireDNSSEC reports whether SSHFP records must be DNSSEC-authenticated
// (default true when unset).
func (c *Config) RequireDNSSEC() bool {
	return c.SSHFPRequireDNSSEC == nil || *c.SSHFPRequireDNSSEC
}

// DashboardRequested reports whether the SPA should be served (default true).
func (c *Config) DashboardRequested() bool {
	return c.Dashboard.Enabled == nil || *c.Dashboard.Enabled
}

// DashboardExplicitlyEnabled reports whether dashboard.enabled was set true.
func (c *Config) DashboardExplicitlyEnabled() bool {
	return c.Dashboard.Enabled != nil && *c.Dashboard.Enabled
}

// ListenIsLoopback reports whether the HTTP listener is bound to loopback.
func (c *Config) ListenIsLoopback() bool { return isLoopback(c.Listen) }

func validDial(d string) bool { return d == DialSSH || d == DialDirect }
func validVerify(v string) bool {
	return v == VerifyKnownHosts || v == VerifySSHFP || v == VerifyInsecure
}
func validCollect(l string) bool { return l == CollectFull || l == CollectTraffic || l == CollectOff }

func isLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}

	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}

	return p
}
