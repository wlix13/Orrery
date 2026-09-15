package main

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/wlix13/orrery/collector/internal/config"
	"github.com/wlix13/orrery/collector/internal/poller"
	"github.com/wlix13/orrery/collector/internal/store"
)

// pollerSet holds running pollers, reconciled against config on every (re)load, touched by one goroutine only.
type pollerSet struct {
	st      store.Store
	log     *slog.Logger
	cfg     *atomic.Pointer[config.Config] // shared with API and retention sweep
	running map[string]*runningPoller
}

// pollerSpec is everything one poller is built from, any change restarts it.
type pollerSpec struct {
	verify string
	node   config.ResolvedNode
	poll   config.PollConfig
	dnssec bool
}

type runningPoller struct {
	closer func() // releases dialer
	cancel context.CancelFunc
	done   chan struct{}
	target poller.Target
	spec   pollerSpec
}

func (r *runningPoller) start(ctx context.Context, st store.Store, log *slog.Logger) {
	ctx, r.cancel = context.WithCancel(ctx)
	r.done = make(chan struct{})

	go func() {
		defer close(r.done)

		poller.New(r.target, st, r.spec.poll, log).Run(ctx)
	}()
}

func (r *runningPoller) halt() {
	r.cancel()
	<-r.done
}

func (r *runningPoller) stop() {
	r.halt()
	r.closer()
}

func release(rs []*runningPoller) {
	for _, r := range rs {
		r.closer()
	}
}

// specsOf keys pollable nodes by node key (collect: off gets no poller).
func specsOf(cfg *config.Config, nodes []config.ResolvedNode) map[string]pollerSpec {
	want := make(map[string]pollerSpec, len(nodes))

	for _, n := range nodes {
		if n.Collect == config.CollectOff {
			continue
		}

		want[n.Key()] = pollerSpec{node: n, poll: cfg.Poll, verify: cfg.HostKeyVerify, dnssec: cfg.RequireDNSSEC()}
	}

	return want
}

// diff compares wanted specs with running ones: stop gone or changed, start new or changed, unchanged keep connection and delta base.
func diff(have, want map[string]pollerSpec) (stop []string, start []pollerSpec) {
	for key, spec := range have {
		if w, ok := want[key]; !ok || w != spec {
			stop = append(stop, key)
		}
	}

	for key, spec := range want {
		if h, ok := have[key]; !ok || h != spec {
			start = append(start, spec)
		}
	}

	return stop, start
}

// apply reconciles pollers and registers nodes. Old pollers halt before registry changes and resume if registering fails.
func (ps *pollerSet) apply(ctx context.Context, cfg *config.Config, nodes []config.ResolvedNode) error {
	have := make(map[string]pollerSpec, len(ps.running))
	for key, r := range ps.running {
		have[key] = r.spec
	}

	stop, start := diff(have, specsOf(cfg, nodes))

	built := make([]*runningPoller, 0, len(start))

	for _, spec := range start {
		t, closer, err := buildTarget(spec.node, spec.verify, spec.dnssec)
		if err != nil {
			release(built)
			return err
		}

		built = append(built, &runningPoller{spec: spec, target: t, closer: closer})
	}

	halted := make([]*runningPoller, 0, len(stop))

	for _, key := range stop {
		ps.running[key].halt()
		halted = append(halted, ps.running[key])
		delete(ps.running, key)
	}

	storeNodes := make([]store.Node, 0, len(nodes))
	for _, n := range nodes {
		storeNodes = append(storeNodes, storeNode(n))
	}

	if err := ps.st.RegisterNodes(ctx, storeNodes); err != nil {
		release(built)
		ps.launch(ctx, halted)

		return err
	}

	release(halted)
	ps.launch(ctx, built)
	ps.cfg.Store(cfg)
	ps.log.Info("config applied", "nodes", len(nodes), "polling", len(ps.running), "started", len(start), "stopped", len(stop))

	return nil
}

func (ps *pollerSet) launch(ctx context.Context, rs []*runningPoller) {
	for _, r := range rs {
		r.start(ctx, ps.st, ps.log)
		ps.running[r.spec.node.Key()] = r
	}
}

func (ps *pollerSet) stopAll() {
	for key, r := range ps.running {
		r.stop()
		delete(ps.running, key)
	}
}
