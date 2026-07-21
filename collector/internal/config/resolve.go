package config

import (
	"fmt"

	"github.com/wlix13/orrery/collector/internal/topology"
)

// ResolvedNode is a fully-determined polling target: topology defaults
// merged with per-node overrides from orrery.yaml.
type ResolvedNode struct {
	Fleet    string
	ID       string
	Region   string
	Type     string
	Hostname string
	Address  string // SSH / gRPC endpoint host
	Dial     string
	Collect  string
	SSH      SSHConfig
	Port     int // Xray API port (as seen from the node for ssh, public for direct)
}

func (n ResolvedNode) Key() string { return n.Fleet + "/" + n.ID }

// applyOverride copies every non-zero override field onto the node.
func (n *ResolvedNode) applyOverride(o NodeOverride) {
	if o.Address != "" {
		n.Address = o.Address
		if n.Hostname == "" {
			n.Hostname = o.Address
		}
	}

	if o.Region != "" {
		n.Region = o.Region
	}

	if o.Type != "" {
		n.Type = o.Type
	}

	if o.Dial != "" {
		n.Dial = o.Dial
	}

	if o.Collect != "" {
		n.Collect = o.Collect
	}

	if o.XrayAPIPort != 0 {
		n.Port = o.XrayAPIPort
	}
}

// ResolveNodes merges the fleet's topology nodes with explicit overrides.
// Overrides may modify topology nodes or add nodes unknown to topology.
func (f *FleetConfig) ResolveNodes(topo []topology.Node) ([]ResolvedNode, error) {
	byID := map[string]*ResolvedNode{}

	var order []string

	for _, tn := range topo {
		byID[tn.ID] = &ResolvedNode{
			Fleet:    f.Name,
			ID:       tn.ID,
			Region:   tn.Region,
			Type:     tn.Type,
			Hostname: tn.Hostname,
			Address:  tn.Hostname,
			Port:     f.XrayAPIPort,
			Dial:     f.Dial,
			SSH:      f.SSH,
		}

		order = append(order, tn.ID)
	}

	for _, o := range f.Nodes {
		n, known := byID[o.ID]
		if !known {
			if o.Type == "" {
				return nil, fmt.Errorf("fleet %q node %q: not in topology and no type given", f.Name, o.ID)
			}

			n = &ResolvedNode{
				Fleet: f.Name,
				ID:    o.ID,
				Type:  o.Type,
				Port:  f.XrayAPIPort,
				Dial:  f.Dial,
				SSH:   f.SSH,
			}
			byID[o.ID] = n

			order = append(order, o.ID)
		}

		n.applyOverride(o)
	}

	nodes := make([]ResolvedNode, 0, len(order))

	for _, id := range order {
		n := byID[id]
		if n.Collect == "" {
			switch n.Type {
			case TypeHub:
				n.Collect = f.Collect.Hub
			default:
				n.Collect = f.Collect.Exit
			}
		}

		if n.Address == "" {
			return nil, fmt.Errorf("fleet %q node %q: no address (topology hostname or explicit address required)", f.Name, n.ID)
		}

		nodes = append(nodes, *n)
	}

	return nodes, nil
}
