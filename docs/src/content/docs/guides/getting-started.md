---
title: Getting started
description: From an empty host to a collector reporting on its first node.
---

Orrery is a single static binary that reads one config file and writes one database.
This page takes one node from nothing to a chart, running the collector in the foreground.
[Deployment and operations](/Orrery/guides/deployment/) turns that into a supervised service.

You need a host that can open SSH connections to the nodes, and nodes running Xray.

## 1. Expose Xray's stats API

Each node needs the `stats`, `api` and `policy` blocks from [Xray stats configuration](/Orrery/guides/xray-stats/), then an Xray restart.

Without them Orrery connects and reads zero counters.

## 2. Give the collector a login on the node

Orrery authenticates as an ordinary SSH user and uses that access for one thing: a port-forward to the node's loopback stats port.

Generate a key on the collector host:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/orrery_ed25519 -N ''
```

Create the user on the node and install the public key, restricted to the stats port:

```bash
sudo useradd --system --create-home --shell /usr/sbin/nologin orrery
```

```text
# ~orrery/.ssh/authorized_keys
restrict,port-forwarding,permitopen="127.0.0.1:10085" ssh-ed25519 AAAA... orrery@collector
```

`restrict` removes agent forwarding, X11, PTY allocation and command execution.
`permitopen` limits the tunnel to the one port Orrery reads.
The account needs no sudo and no shell.

## 3. Install the collector

```bash
curl -fsSLO https://github.com/wlix13/Orrery/releases/latest/download/orrery-linux-amd64
curl -fsSLO https://github.com/wlix13/Orrery/releases/latest/download/SHA256SUMS
sha256sum --ignore-missing --check SHA256SUMS
mv orrery-linux-amd64 orrery && chmod +x orrery
```

Assets are `orrery-linux-{amd64,arm64}`, each with a `-nodashboard` variant that carries no SPA, plus `SHA256SUMS` covering all of them.
Swap `latest/download` for `download/v0.1.0` to pin a version.

## 4. Write a config

The smallest useful `orrery.yaml` names one fleet and one node inline:

```yaml
listen: "127.0.0.1:9800"
db: orrery.db
auth:
  tokens:
    - name: me
      token: "${ORRERY_TOKEN}"

host_key_verify: known_hosts

fleets:
  - name: lab
    ssh:
      user: orrery
      key_file: ~/.ssh/orrery_ed25519
    nodes:
      - id: hub01
        type: hub
        address: hub01.example.net
```

Inline nodes need `type` and `address`; a fleet reading a topology file infers both.

Values are env-expanded, so keep the token out of the file:

```bash
export ORRERY_TOKEN=$(openssl rand -hex 32)
```

Record the node's host key, since `host_key_verify` defaults to `known_hosts`:

```bash
ssh-keyscan hub01.example.net >> ~/.ssh/known_hosts
```

Fleets whose nodes publish DNS SSHFP records can use `host_key_verify: sshfp` and skip that file.

Every setting is in the [configuration reference](/Orrery/reference/configuration/), and [`orrery.example.yaml`](https://github.com/wlix13/Orrery/blob/main/orrery.example.yaml) is an annotated copy to start from.

## 5. Probe the node

`probe` reads one node over its configured dial path and writes nothing, so it checks the wiring without starting the collector:

```bash
./orrery -config orrery.yaml probe lab/hub01
```

```text
node lab/hub01 (hub, ssh dial): 24 counters
  inbound>>>vless-in>>>traffic>>>uplink                    18272634
  inbound>>>vless-in>>>traffic>>>downlink                  204817263
  user>>>alice@example>>>traffic>>>uplink                  9182736
  ...
```

Nodes are addressed as `fleet/id`, which is why the argument is `lab/hub01`.

`0 counters` means Xray is not counting: go back to step 1.
Any other failure is a connection problem - see [Troubleshooting](/Orrery/guides/troubleshooting/).

## 6. Run it

```bash
./orrery -config orrery.yaml serve
```

Open `http://127.0.0.1:9800` and paste the token into the gate.
Counters are cumulative and Orrery stores deltas, so the first poll sets the baseline and the second produces the first data point.

## Command line

```text
orrery [-config path] <serve | probe <fleet/id> | version>
```

| Command | What it does |
|---|---|
| `serve` | Registers nodes, polls them, serves the API, dashboard and `/metrics`. |
| `probe <fleet/id>` | One-shot read of a single node, printed to stdout. Touches no database. |
| `version` | Prints the version stamped into the binary. |

| Flag | Default | What it does |
|---|---|---|
| `-config` | `orrery.yaml` | Path to the config file. Accepted before or after the command. |

## Where next

- [Deployment and operations](/Orrery/guides/deployment/) - service user, systemd, backups, exposing the dashboard, scoping access.
- [Architecture](/Orrery/reference/architecture/) - how polling, storage and scoping work.
