// Command orrery collects Xray StatsService metrics across the
// Conglomerate fleet and serves the API + dashboard.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wlix13/orrery/collector/internal/api"
	"github.com/wlix13/orrery/collector/internal/config"
	"github.com/wlix13/orrery/collector/internal/poller"
	"github.com/wlix13/orrery/collector/internal/sshdial"
	"github.com/wlix13/orrery/collector/internal/store"
	"github.com/wlix13/orrery/collector/internal/store/mongo"
	"github.com/wlix13/orrery/collector/internal/store/sqlite"
	"github.com/wlix13/orrery/collector/internal/topology"
	"github.com/wlix13/orrery/collector/internal/xray"
)

var version = "dev" // set via -ldflags "-X main.version=..."

// openStore picks the storage backend from the db setting: a mongodb://
// (or mongodb+srv://) URI selects MongoDB, anything else is a SQLite path.
func openStore(dsn string) (store.Store, error) {
	if strings.HasPrefix(dsn, "mongodb://") || strings.HasPrefix(dsn, "mongodb+srv://") {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return mongo.Open(ctx, dsn)
	}

	return sqlite.Open(dsn)
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	fs := flag.NewFlagSet("orrery", flag.ExitOnError)
	cfgPath := fs.String("config", "orrery.yaml", "path to orrery.yaml")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: orrery [-config path] <serve|probe <fleet/id>|version>\n")
		fs.PrintDefaults()
	}

	args := os.Args[1:]
	// Support both "orrery -config x serve" and "orrery serve -config x".
	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		fs.Usage()
		return errors.New("missing command")
	}

	cmd, rest := rest[0], rest[1:]
	if err := fs.Parse(rest); err != nil {
		return err
	}

	rest = fs.Args()

	if cmd == "version" {
		fmt.Println("orrery", version)
		return nil
	}

	cfg, nodes, err := load(*cfgPath)
	if err != nil {
		return err
	}

	warnConfig(cfg, log)

	switch cmd {
	case "serve":
		return serve(cfg, nodes, *cfgPath, log)
	case "probe":
		if len(rest) != 1 {
			return errors.New("usage: orrery probe <fleet/id>")
		}

		return probe(cfg, nodes, rest[0], log)
	default:
		fs.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// load reads config and resolves every fleet's nodes.
func load(path string) (*config.Config, []config.ResolvedNode, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}

	nodes, err := resolveAll(cfg)
	if err != nil {
		return nil, nil, err
	}

	return cfg, nodes, nil
}

// warnConfig flags settings that must not reach production by accident.
func warnConfig(cfg *config.Config, log *slog.Logger) {
	if cfg.HostKeyVerify == config.VerifyInsecure {
		log.Warn("SSH host key verification is DISABLED (host_key_verify: insecure) - do not use in production")
	}

	if cfg.Auth.AllowAnonymous {
		log.Warn("API and /metrics are UNAUTHENTICATED (auth.allow_anonymous)",
			"listen", cfg.Listen, "loopback", cfg.ListenIsLoopback())
	} else {
		log.Info("auth configured", "tokens", len(cfg.Auth.Tokens))
	}
}

func resolveAll(cfg *config.Config) ([]config.ResolvedNode, error) {
	var all []config.ResolvedNode

	for i := range cfg.Fleets {
		f := &cfg.Fleets[i]

		var topo []topology.Node

		if f.Topology != "" {
			var err error

			topo, err = topology.Load(f.Topology)
			if err != nil {
				return nil, fmt.Errorf("fleet %q: %w", f.Name, err)
			}
		}

		nodes, err := f.ResolveNodes(topo)
		if err != nil {
			return nil, err
		}

		all = append(all, nodes...)
	}

	return all, nil
}

func storeNode(n config.ResolvedNode) store.Node {
	return store.Node{
		Key: n.Key(), Fleet: n.Fleet, ID: n.ID, Region: n.Region,
		Type: n.Type, Hostname: n.Hostname, Collect: n.Collect,
	}
}

// buildTarget wires a node's dialer. The returned closer releases the SSH
// connection (no-op for direct dial).
func buildTarget(n config.ResolvedNode, verify string, requireDNSSEC bool) (poller.Target, func(), error) {
	target := poller.Target{Node: storeNode(n), Collect: n.Collect}

	switch n.Dial {
	case config.DialDirect:
		target.Addr = fmt.Sprintf("%s:%d", n.Address, n.Port)
		target.Dial = poller.DirectDial()

		return target, func() {}, nil
	default: // ssh
		d, err := sshdial.New(sshdial.Options{
			Host: n.Address, Port: n.SSH.Port, User: n.SSH.User,
			KeyFile: n.SSH.KeyFile, KnownHostsFile: n.SSH.KnownHosts,
			Verify: verify, RequireDNSSEC: requireDNSSEC,
		})
		if err != nil {
			return poller.Target{}, nil, fmt.Errorf("node %s: %w", n.Key(), err)
		}
		// Tunnel target is the node's loopback listener.
		target.Addr = fmt.Sprintf("127.0.0.1:%d", n.Port)
		target.Dial = d.DialContext

		return target, func() { d.Close() }, nil
	}
}

func serve(cfg *config.Config, nodes []config.ResolvedNode, cfgPath string, log *slog.Logger) error {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	defer signal.Stop(hup)

	st, err := openStore(cfg.DB)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Live config: apply stores it, API and retention sweep read it.
	var current atomic.Pointer[config.Config]

	ps := &pollerSet{st: st, log: log, cfg: &current, running: map[string]*runningPoller{}}

	if err := ps.apply(ctx, cfg, nodes); err != nil {
		return err
	}

	var wg sync.WaitGroup

	wg.Go(func() { ps.watch(ctx, cfgPath, hup) })

	// Retention sweep hourly.
	wg.Go(func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			r := current.Load().Retention
			if err := st.Retention(ctx, r.Minute.D(), r.Hour.D()); err != nil {
				log.Error("retention", "err", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.New(st, &current, version, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("orrery serving", "listen", cfg.Listen, "version", version)

	err = srv.ListenAndServe()

	// Reload loop must be idle before pollers are torn down.
	stop()
	wg.Wait()
	ps.stopAll()

	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	log.Info("shut down cleanly")

	return nil
}

// probe polls one node once and prints a human summary - for smoke-testing
// connectivity and config before running serve.
func probe(cfg *config.Config, nodes []config.ResolvedNode, key string, log *slog.Logger) error {
	var found *config.ResolvedNode

	for i := range nodes {
		if nodes[i].Key() == key {
			found = &nodes[i]
			break
		}
	}

	if found == nil {
		return fmt.Errorf("unknown node %q (known: use <fleet>/<id>)", key)
	}

	t, closer, err := buildTarget(*found, cfg.HostKeyVerify, cfg.RequireDNSSEC())
	if err != nil {
		return err
	}

	defer closer()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Poll.Timeout.D())
	defer cancel()

	client, err := xray.New(t.Addr, t.Dial)
	if err != nil {
		return err
	}
	defer client.Close()

	stats, err := client.QueryAll(ctx)
	if err != nil {
		return fmt.Errorf("query stats via %s dial: %w", found.Dial, err)
	}

	fmt.Printf("node %s (%s, %s dial): %d counters\n", key, found.Type, found.Dial, len(stats))

	for _, s := range stats {
		fmt.Printf("  %-70s %d\n", s.Name, s.Value)
	}

	if sys, err := client.SysStats(ctx); err == nil {
		fmt.Printf("xray uptime %s, %d goroutines, %.1f MiB alloc\n",
			(time.Duration(sys.UptimeS) * time.Second).String(), sys.NumGoroutine, float64(sys.Alloc)/(1<<20))
	} else {
		fmt.Printf("sys stats unavailable: %v\n", err)
	}

	if online, supported, err := client.OnlineUsers(ctx); err != nil {
		fmt.Printf("online users unavailable: %v\n", err)
	} else if !supported {
		fmt.Println("online users: not supported by this Xray version")
	} else {
		fmt.Printf("online users: %d\n", len(online))

		for _, u := range online {
			fmt.Printf("  %s (%d IPs)\n", u.Email, len(u.IPs))
		}
	}

	return nil
}
