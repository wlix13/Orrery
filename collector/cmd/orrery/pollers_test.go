package main

import (
	"slices"
	"testing"

	"github.com/wlix13/orrery/collector/internal/config"
)

func TestDiff(t *testing.T) {
	spec := func(id, addr string) pollerSpec {
		return pollerSpec{node: config.ResolvedNode{Fleet: "main", ID: id, Address: addr}}
	}

	have := map[string]pollerSpec{"main/a": spec("a", "1"), "main/b": spec("b", "1"), "main/c": spec("c", "1")}
	want := map[string]pollerSpec{"main/a": spec("a", "1"), "main/b": spec("b", "2"), "main/d": spec("d", "1")}

	stop, start := diff(have, want)

	started := make([]string, 0, len(start))
	for _, s := range start {
		started = append(started, s.node.Key())
	}

	slices.Sort(stop)
	slices.Sort(started)

	// a unchanged, b changed (restart), c gone, d new.
	if !slices.Equal(stop, []string{"main/b", "main/c"}) || !slices.Equal(started, []string{"main/b", "main/d"}) {
		t.Fatalf("stop = %v, start = %v", stop, started)
	}
}
