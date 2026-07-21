// Package topology reads the minimal subset of a HexRift topology.yaml
// that Orrery needs: regions[].{id,type} and their nodes[].{id,hostname}.
package topology

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Node struct {
	ID       string
	Hostname string
	Region   string
	Type     string // "hub" | "exit"
}

type file struct {
	Regions []struct {
		ID    string `yaml:"id"`
		Type  string `yaml:"type"`
		Nodes []struct {
			ID       string `yaml:"id"`
			Hostname string `yaml:"hostname"`
		} `yaml:"nodes"`
	} `yaml:"regions"`
}

// Load parses the topology file. Unknown keys are ignored on purpose -
// HexRift owns the schema; Orrery only consumes identity and addressing.
func Load(path string) ([]Node, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var f file
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse topology %s: %w", path, err)
	}

	var nodes []Node

	for _, r := range f.Regions {
		if r.Type != "hub" && r.Type != "exit" {
			return nil, fmt.Errorf("topology %s: region %q has unsupported type %q", path, r.ID, r.Type)
		}

		for _, n := range r.Nodes {
			if n.ID == "" || n.Hostname == "" {
				return nil, fmt.Errorf("topology %s: region %q has a node missing id or hostname", path, r.ID)
			}

			nodes = append(nodes, Node{ID: n.ID, Hostname: n.Hostname, Region: r.ID, Type: r.Type})
		}
	}

	return nodes, nil
}
