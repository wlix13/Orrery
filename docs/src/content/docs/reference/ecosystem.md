---
title: Ecosystem
description: What Orrery consumes from the rest of the fleet, and what it leaves to other tools.
---

Orrery is read-only.
It generates no configuration, changes nothing on a node, and depends on two things existing: Xray serving counters, and an SSH account to reach them through.

Neither has to come from any particular tool.
A fleet built by hand needs only nodes listed under `fleets[].nodes` and a login to use.

## Sibling projects

Orrery is one of several tools sharing the Conglomerate fleet.
Two of them produce what it consumes:

- **[HexRift](https://github.com/wlix13/HexRift)** generates each node's Xray `config.json` from a `topology.yaml`. That makes it the source of both halves of the integration: the node list Orrery reads, and the `stats`/`api`/`policy` blocks it polls, which HexRift's `observability` feature emits.
- **[NullForge](https://github.com/wlix13/NullForge)** is the pyinfra-based provisioning layer for the fleet. Installing the collector and creating its per-node login are provisioning work of that kind.

Each documents its own configuration.
This page covers only the surface Orrery depends on; the fleet-wide picture belongs in the Conglomerate documentation.

## The topology contract

A fleet can take its node list from a topology file instead of `orrery.yaml`.
Orrery reads a deliberately small slice of it and ignores everything else:

```yaml
regions:
  - id: nl              # -> node region
    type: exit          # -> node type; must be hub or exit
    nodes:
      - id: nlA00       # -> node id, the second half of the nlA00 node key
        hostname: nlA00.example.net   # -> SSH / gRPC address
```

That is the whole contract: `regions[].id`, `regions[].type`, `nodes[].id`, `nodes[].hostname`.
A region typed as anything other than `hub` or `exit` fails to load, as does a node missing an id or hostname.

The type is not just a label.
Hubs default to `collect: full` and are the only nodes the API reports per-user data for; exits default to `collect: traffic`.

Per-node entries in `orrery.yaml` layer on top, overriding a topology node by `id` or adding one the file does not list:

```yaml
fleets:
  - name: main
    topology: /etc/orrery/topology.yaml
    nodes:
      - id: hub01
        collect: off         # registered, never polled
      - id: lab00            # absent from topology, so type is required
        type: exit
        address: 203.0.113.7
        dial: direct
```

This is the format HexRift generates, which is why fleets it manages need no node list in `orrery.yaml`.

## What Orrery leaves to other tools

- **Config management and node control.** The API is read-only.
- **Host metrics.** CPU, disk and network belong to a host-monitoring agent. Orrery reports what Xray knows, plus Xray's own process stats.
- **Alerting.** Enable `metrics.enabled` and let Prometheus and Alertmanager do it.
- **Writes to Xray.** Counters are read without resetting them, so Orrery coexists with other consumers of the same stats API.
