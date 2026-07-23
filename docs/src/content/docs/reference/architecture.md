---
title: Architecture
description: How the collector, storage and dashboard fit together.
---


Metrics collection and visualization for the **Conglomerate** proxy fleet.

## Goals

- Collect per-node Xray metrics via Xray's gRPC `StatsService`: traffic per inbound/outbound tag, per-user traffic and online presence (hubs), process health (`GetSysStats`).
- Hub nodes: full detail (per-inbound, per-user, online users). Exit nodes: tag-level traffic by default; per-hub attribution optional; collection can be disabled per node.
- One collector serves multiple fleets from their existing topology files.
- Dashboard deployable **both** self-hosted (embedded in the collector binary) and on **Cloudflare Workers** (static assets + thin auth proxy).
- Optional Prometheus exposition so an existing Grafana can also scrape.
- Multiple credentials, each scoped to a set of fleets.

## Non-goals (v1)

- No config management or node control - see [Ecosystem](/Orrery/reference/ecosystem/).
- No host-level metrics (CPU/disk).
- No alerting; no sys-stats history (only last value).

## System context

```text
                    ┌────────────────────────── collector host ─────────────┐
                    │                                                       │
  ┌──────────┐ SSH  │  ┌─────────────────── orrery serve ────────────────┐  │
  │ hub node │◄─────┼──┤ per-node pollers ──► SQLite ──► HTTP API + SPA  │  │
  │ (xray)   │ gRPC │  │ (SSH tunnel → 127.0.0.1:<api_port> gRPC)        │  │
  └──────────┘ over │  └─────────────────────────────────────────────────┘  │
  ┌──────────┐ tun  │                  ▲ bearer token                       │
  │ exit node│◄─────┼───────────────────────┼────────────────────────────── │
  └──────────┘      └───────────────────────┼───────────────────────────────┘
                                            │ HTTPS (direct or CF Tunnel)
                          ┌─────────────────┴───────────┐
                          │ Cloudflare Worker (optional)│
                          │ static SPA + /api/* proxy   │
                          │ terminates CF Access        │
                          └─────────────────────────────┘
```

Two dial modes, chosen per fleet (overridable per node):

- **`ssh`** (default): Xray's gRPC API binds loopback (`api.listen: 127.0.0.1:<port>`) and the collector dials gRPC through its own SSH client, so there are no `ssh -L` processes and no firewall changes.
- **`direct`**: Xray binds the API on a routable address and the node firewall allows the collector's addresses to that port only. Simpler runtime, but the commander listener is plain TCP with **no TLS or auth on the wire**. Fine for tag-level counters; think twice before sending per-user emails and IPs this way.

## Xray prerequisites

Xray counts nothing unless its config asks it to.
Each node needs `stats`, `api` (the `StatsService` gRPC listener) and `policy` (which counters exist) - see [Xray stats configuration](/Orrery/guides/xray-stats/).

## Collector

Pure Go and CGO-free, so releases are static binaries.

```text
collector/
  cmd/orrery/            main: serve | probe <node> | version
  internal/config/       orrery.yaml load + validation + defaults
  internal/topology/     minimal topology.yaml reader (regions[].nodes[])
  internal/xray/command/ vendored StatsService protobuf stubs (from Xray-core, MPL-2.0)
  internal/xray/         thin client: QueryAll, SysStats, OnlineUsers (w/ version fallbacks)
  internal/sshdial/      per-node SSH connection manager (keepalive, backoff, host keys)
  internal/poller/       schedules polls, parses counters, computes deltas
  internal/store/        storage contract + shared types
  internal/store/sqlite/ default embedded backend
  internal/store/mongo/  optional MongoDB backend (mongodb:// db URI)
  internal/store/storetest/ backend conformance suite
  internal/api/          HTTP API, auth middleware, embedded SPA
  internal/promexp/      optional /metrics text exposition
```

### Polling

- One goroutine per node, jittered ticker (`poll.interval`, default 60s), per-poll timeout (default 15s).
- Each poll: `QueryStats(pattern:"", reset:false)` → all counters; `GetSysStats`; if the collect level allows: online users via `GetUsersStats`, falling back to `GetAllOnlineUsers` (+`GetStatsOnlineIpList`), degrading gracefully on `Unimplemented` (older Xray).
- Delta per counter vs. the last persisted value; if `current < last`, Xray restarted → delta = current. Last values are persisted, so collector restarts do not double-count.
- Counter names parse as `kind>>>entity>>>traffic>>>direction` (kind: `inbound|outbound|user`). Unknown shapes are skipped, logged once.
- Collect levels: `full` (tags + users + online + sys), `traffic` (tags + sys), `off`. Defaults: hub → `full`, exit → `traffic`.

### Storage

The contract is the `store.Store` interface; `store/sqlite` and `store/mongo` implement it and both must pass the `store/storetest` conformance suite.
Selection is by the `db` setting: a `mongodb://` or `mongodb+srv://` URI picks MongoDB (database name from the URI path, default `orrery`), anything else is a SQLite file path.

Buckets are written at ingest into **both** minute and hour resolution (upsert `bytes += delta`), so there is no rollup job and retention is one delete per bucket table.

MongoDB caveat: standalone servers have no multi-document transactions, so `WriteSample` is ordered best-effort - the counter delta-base is persisted *first*, so a crash mid-write can under-count one poll interval but never double-count.
SQLite writes stay fully transactional.

```text
nodes           node_key PK (fleet/id), fleet, id, region, type, hostname,
                collect level, last_ok, last_err, sys-stat snapshot columns
counters_last   node_key, name → value, ts        (delta base)
traffic_minute  bucket_ts, node_key, kind, entity, dir → bytes
traffic_hour    bucket_ts, node_key, kind, entity, dir → bytes
online_minute   bucket_ts, node_key → count        (gauge, last-write-wins)
online_current  node_key, email, ip, last_seen     (snapshot per poll)
```

Node status is derived at request time rather than stored: `up` if the last successful poll is under 2× the poll interval old, `stale` under 5×, else `down`.
A node with `collect: off` reports `off` instead.

### Configuration (`orrery.yaml`)

```yaml
listen: "127.0.0.1:9800"
auth:
  tokens:                           # each token is a principal; omit fleets for all
    - { name: ops, token: "${ORRERY_TOKEN}" }
    - { name: caas, token: "${ORRERY_CAAS_TOKEN}", fleets: [caas] }
db: /var/lib/orrery/orrery.db
poll: { interval: 60s, timeout: 15s }
retention: { minute: 72h, hour: 2160h }
metrics: { enabled: true }        # Prometheus /metrics (same bearer auth)
dashboard: { enabled: true }      # serve the embedded SPA at /
host_key_verify: sshfp            # known_hosts | sshfp | insecure
fleets:
  - name: main
    topology: /etc/orrery/topology.yaml   # nodes derived from regions[].nodes[]
    xray_api_port: 10085
    dial: ssh                                   # fleet default: ssh | direct
    ssh: { user: orrery, key_file: /var/lib/orrery/.ssh/orrery_ed25519, port: 22,
           known_hosts: ~/.ssh/known_hosts }
    collect: { hub: full, exit: traffic }       # per-type defaults
    nodes:                                      # optional per-node overrides / additions
      - id: hub01
        collect: off
      - id: labX00                              # node not in topology
        address: 203.0.113.7
        type: exit
        dial: direct                            # skip SSH, dial gRPC directly
```

Address defaults to the topology `hostname`; explicit `address` overrides.
Every setting is in the [configuration reference](/Orrery/reference/configuration/), and the slice of the topology file this reads is in [Ecosystem](/Orrery/reference/ecosystem/#the-topology-contract).

## HTTP API

A small read-only JSON API, documented endpoint by endpoint in the [HTTP API reference](/Orrery/reference/api/).

`/api/overview`, `/api/nodes` and `/api/users` each answer one page's question in one request.
`/api/series` is the general query surface: a range, a step and a group-by in, dense point arrays out, read from the minute or hour table according to the step.
`GET /metrics` is the same data in Prometheus exposition, behind the same token and scope.

## Dashboard

Vite + React + TypeScript + Tailwind CSS v4, uPlot for charts.
Four pages: Overview, Nodes, Users, Settings.

Each entry under `auth.tokens` is a principal, and every read is scoped to its fleets.
The filtering is server-side, so the dashboard carries no scoping logic: a single-fleet principal receives single-fleet data, and the UI drops the `fleet/` prefix and the fleet filter accordingly.

Single sign-on lives at the edge, not in the collector, which authenticates bearer tokens and nothing else.
Self-hosted, the SPA sends the token from its token gate.
On Cloudflare, the Worker authenticates the visitor - a Cloudflare Access assertion or its own `DASHBOARD_TOKEN` - and calls the origin with the collector token mapped to that identity, so the collector token never reaches the browser.

One build, two deploy targets:

- **Self-host**: `dashboard/dist` is copied into `collector/internal/api/webui/` and embedded via `go:embed`, with SPA fallback to `index.html`. `dashboard.enabled: false` turns serving off at runtime; `-tags nodashboard` builds a collector that never carried the SPA.
- **Cloudflare**: `dashboard/worker/` serves the same assets and proxies `/api/*`, adding the auth header server-side.
