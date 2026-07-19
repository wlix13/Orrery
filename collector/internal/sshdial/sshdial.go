// Package sshdial maintains one lazy, self-healing SSH connection per node
// and exposes it as a dialer for gRPC: each gRPC (re)connect becomes a
// direct-tcpip channel to the node's loopback Xray API listener.
package sshdial

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const dialTimeout = 10 * time.Second

// Host-key verification modes (mirror config's Verify* constants).
const (
	VerifyKnownHosts = "known_hosts"
	VerifySSHFP      = "sshfp"
	VerifyInsecure   = "insecure"
)

type Options struct {
	Host           string // SSH endpoint (hostname or IP)
	User           string
	KeyFile        string
	KnownHostsFile string
	Verify         string // known_hosts | sshfp | insecure
	Port           int
	RequireDNSSEC  bool // sshfp: reject records without a DNSSEC-authenticated (AD) response
}

// Dialer is safe for concurrent use, though in practice each node's poller
// is its only caller.
type Dialer struct {
	conf *ssh.ClientConfig

	client *ssh.Client
	addr   string
	// sshfpHost is the lookup name in sshfp mode, empty otherwise.
	sshfpHost string

	mu sync.Mutex
}

func New(o Options) (*Dialer, error) {
	key, err := os.ReadFile(o.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key %s: %w", o.KeyFile, err)
	}

	hostKeyCallback, err := newHostKeyCallback(o)
	if err != nil {
		return nil, err
	}

	// Hostnames are case-insensitive; lower-case so the known_hosts/SSHFP
	// lookup matches ssh-keyscan and OpenSSH (Go's knownhosts is case-sensitive).
	host := strings.ToLower(o.Host)

	d := &Dialer{
		addr: net.JoinHostPort(host, fmt.Sprint(o.Port)),
		conf: &ssh.ClientConfig{
			User:            o.User,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: hostKeyCallback,
			Timeout:         dialTimeout,
		},
	}

	if o.Verify == VerifySSHFP {
		d.sshfpHost = host
	}

	return d, nil
}

// pinSSHFPAlgorithms restricts negotiation to the host-key algorithms the node
// publishes SSHFP records for. Best effort: on lookup failure the handshake
// proceeds and the callback still rejects any key without a matching record.
func (d *Dialer) pinSSHFPAlgorithms() {
	if d.sshfpHost == "" {
		return
	}

	records, _, err := lookupSSHFP(d.sshfpHost)
	if err != nil {
		return
	}

	if algos := hostKeyAlgorithms(records); len(algos) > 0 {
		d.conf.HostKeyAlgorithms = algos
	}
}

func newHostKeyCallback(o Options) (ssh.HostKeyCallback, error) {
	switch o.Verify {
	case VerifyInsecure:
		return ssh.InsecureIgnoreHostKey(), nil // #nosec G106 -- explicit opt-in, warned at startup
	case VerifySSHFP:
		return sshfpCallback(o.RequireDNSSEC), nil
	default: // known_hosts
		cb, err := knownhosts.New(o.KnownHostsFile)
		if err != nil {
			return nil, fmt.Errorf("load known_hosts %s: %w", o.KnownHostsFile, err)
		}

		return cb, nil
	}
}

// DialContext opens a tunneled connection to target (host:port as seen
// from the node, e.g. "127.0.0.1:10085"). A dead SSH session is replaced
// once per call; a second failure is returned to the caller (the poller
// retries next tick).
func (d *Dialer) DialContext(ctx context.Context, target string) (net.Conn, error) {
	client, err := d.ensureClient(ctx)
	if err != nil {
		return nil, err
	}

	conn, err := client.DialContext(ctx, "tcp", target)
	if err == nil {
		return conn, nil
	}
	// The session may have died since the last poll - rebuild once.
	d.invalidate(client)

	client, rerr := d.ensureClient(ctx)
	if rerr != nil {
		return nil, fmt.Errorf("ssh tunnel to %s: %w (after reconnect failed: %v)", target, err, rerr)
	}

	return client.DialContext(ctx, "tcp", target)
}

func (d *Dialer) ensureClient(ctx context.Context) (*ssh.Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.client != nil {
		return d.client, nil
	}

	d.pinSSHFPAlgorithms()

	netConn, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", d.addr)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", d.addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, d.addr, d.conf)
	if err != nil {
		netConn.Close()
		return nil, fmt.Errorf("ssh handshake %s: %w", d.addr, err)
	}

	d.client = ssh.NewClient(sshConn, chans, reqs)

	return d.client, nil
}

func (d *Dialer) invalidate(dead *ssh.Client) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.client == dead {
		_ = d.client.Close() // best effort; the session is already dead
		d.client = nil
	}
}

func (d *Dialer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.client != nil {
		err := d.client.Close()
		d.client = nil

		return err
	}

	return nil
}
