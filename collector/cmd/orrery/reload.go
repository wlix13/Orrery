package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"time"

	"github.com/wlix13/orrery/collector/internal/config"
)

// reloadCheck is how often config and topology files are stat'ed for changes.
const reloadCheck = 10 * time.Second

// watch reloads config on SIGHUP and whenever config or topology files change on disk.
func (ps *pollerSet) watch(ctx context.Context, path string, hup <-chan os.Signal) {
	ticker := time.NewTicker(reloadCheck)
	defer ticker.Stop()

	stamp := ps.stamp(path)

	for {
		select {
		case <-ctx.Done():
			return
		case <-hup:
			ps.log.Info("reloading config", "trigger", "SIGHUP")
		case <-ticker.C:
			if maps.Equal(ps.stamp(path), stamp) {
				continue
			}

			ps.log.Info("reloading config", "trigger", "file changed")
		}

		stamp = ps.reload(ctx, path, stamp)
	}
}

// reload applies on-disk config, returns stamp to watch from (old one when reload fails, so next tick retries).
func (ps *pollerSet) reload(ctx context.Context, path string, stamp map[string]string) map[string]string {
	// Stamped before reading, so writes landing mid-read retry next tick.
	before := ps.stamp(path)
	old := ps.cfg.Load()

	cfg, nodes, err := load(path)
	if err == nil {
		err = ps.apply(ctx, cfg, nodes)
	}

	if err != nil {
		ps.log.Error("config reload failed, keeping the running config", "err", err)
		return stamp
	}

	warnConfig(cfg, ps.log)

	if changed := restartOnly(old, cfg); len(changed) > 0 {
		ps.log.Warn("changed settings need a restart to take effect", "settings", changed)
	}

	fresh := ps.stamp(path)
	for p := range fresh {
		if s, ok := before[p]; ok {
			fresh[p] = s
		}
	}

	return fresh
}

// stamp fingerprints config file plus every topology file it names, keyed by path.
func (ps *pollerSet) stamp(path string) map[string]string {
	out := map[string]string{path: fileStamp(path)}

	for _, f := range ps.cfg.Load().Fleets {
		if f.Topology != "" {
			out[f.Topology] = fileStamp(f.Topology)
		}
	}

	return out
}

func fileStamp(p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		return err.Error()
	}

	return fmt.Sprintf("%d:%d", fi.ModTime().UnixNano(), fi.Size())
}

// restartOnly names settings wired once at startup that differ between running and reloaded config.
func restartOnly(old, cur *config.Config) []string {
	var changed []string

	if old.Listen != cur.Listen {
		changed = append(changed, "listen")
	}

	if old.DB != cur.DB {
		changed = append(changed, "db")
	}

	if old.Metrics != cur.Metrics {
		changed = append(changed, "metrics")
	}

	if old.DashboardRequested() != cur.DashboardRequested() {
		changed = append(changed, "dashboard")
	}

	return changed
}
