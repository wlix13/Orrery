---
title: HTTP API
description: Every endpoint the collector serves, with its parameters and response shape.
---

The collector serves a small read-only JSON API, the same one the dashboard uses.

Conventions:

- Timestamps are **Unix seconds**, sizes are **bytes**.
- Nodes are addressed by their node key, `fleet/id`.
- Ranges are half-open, `[from, to)`.
- Arrays are always arrays; an empty result is `[]`, never `null`.

## Authentication

`/api/*` and `/metrics` require `Authorization: Bearer <token>`, whatever the listen address.
Serving without credentials takes an explicit `auth.allow_anonymous: true`.
`GET /healthz` is the only route that never requires a credential.

Every read is scoped to the fleets the calling principal may see, and aggregates - Overview totals, top-N lists, `/metrics` - are computed inside that scope.

## Errors

```json
{"error": {"code": "bad_range", "message": "range must be one of 1h|6h|24h|7d|30d|90d"}}
```

| Code | Status | When |
|---|---|---|
| `unauthorized` | 401 | Missing or unknown credential. |
| `not_found` | 404 | Unknown node key, an unrouted path, or `/` when no dashboard is served. |
| `bad_range` | 400 | `range` is not one of the accepted values, or `from >= to`. |
| `bad_seen` | 400 | `seen` is not one of the accepted values. |
| `bad_kind` | 400 | `kind` is not `inbound`, `outbound`, `user` or `online`. |
| `bad_step` | 400 | `step` below 60 seconds. |
| `bad_query` | 400 | Rejected by the store, e.g. an unknown `agg` or too many slots. |
| `internal` | 500 | Storage failure. Details go to the collector log, not the response. |

## Common parameters

| Parameter | Applies to | Default | Values |
|---|---|---|---|
| `range` | overview, node detail, users, user detail | `1h` | `1h`, `6h`, `24h`, `7d`, `30d`, `90d` |
| `fleet` | every scoped read | none | Narrows to one fleet within the caller's scope. A fleet outside the scope returns empty results rather than an error. |
| `seen` | users, user detail | `6h` | `1h`, `6h`, `24h`. Lookback for which hubs a user has been seen on, independent of the traffic range. |

## Endpoints

### `GET /healthz`

Unauthenticated liveness.

```json
{"status": "ok", "version": "1.2.3", "uptime_s": 84213}
```

### `GET /api/me`

Who the caller is and what it may see.
The dashboard calls this on boot to decide whether to show its token gate.

```json
{"name": "ops", "method": "token", "fleets": null}
```

`method` is `token` or `anonymous`.
`fleets: null` means every fleet; otherwise it is the list the principal is scoped to.
The Worker dashboard rewrites `name` and `method` to the identity it verified.

### `GET /api/overview`

Everything the Overview page needs in one request.
Parameters: `range`, `fleet`.

```json
{
  "generated_at": 1721740800,
  "nodes": {"total": 12, "up": 11, "stale": 0, "down": 0, "off": 1},
  "online_users": 34,
  "totals": {"up_bytes": 128374, "down_bytes": 9182736},
  "fleets": [{"fleet": "main", "nodes_up": 11, "nodes_total": 12, "up_bytes": 1, "down_bytes": 2}],
  "top_users": [{"email": "alice@example", "up_bytes": 1, "down_bytes": 2}],
  "top_nodes": [{"node": "main/hub01", "up_bytes": 1, "down_bytes": 2}]
}
```

`fleets` is sorted by name.
`top_users` (hubs only) and `top_nodes` (inbound traffic) hold at most ten entries each.

### `GET /api/nodes`

Every node in scope.
Parameters: `fleet`.

```json
[{
  "node": "main/hub01", "fleet": "main", "id": "hub01",
  "region": "msk", "type": "hub", "hostname": "hub01.example.net",
  "status": "up", "collect": "full", "last_ok": 1721740780,
  "uptime_s": 831201, "num_goroutine": 61,
  "alloc_bytes": 20971520, "sys_bytes": 41943040, "num_gc": 812
}]
```

A failing node carries `last_err` and `last_err_ts`; both are omitted when there is no error.
`last_err` is a coarse label (`node unreachable`, `xray api timed out`, `storage write failed`, ...), not the error text, which is logged instead.
The `uptime_s`, `num_goroutine`, `alloc_bytes`, `sys_bytes` and `num_gc` fields are Xray's process stats from the last successful poll, not host metrics.

`status` is derived at request time from `last_ok` and the configured poll interval:

| Status | Meaning |
|---|---|
| `up` | Last successful poll under 2 poll intervals ago. |
| `stale` | Under 5 intervals. |
| `down` | Older than that, or never polled successfully. |
| `off` | `collect: off`. Never polled. |

### `GET /api/nodes/{fleet}/{id}`

One node, plus its traffic totals over the range.
Parameters: `range`.

```json
{
  "node": "main/hub01", "...": "all fields from /api/nodes",
  "inbounds":  [{"entity": "vless-in", "up_bytes": 1, "down_bytes": 2}],
  "outbounds": [{"entity": "direct",   "up_bytes": 1, "down_bytes": 2}],
  "users":     [{"email": "alice@example", "up_bytes": 1, "down_bytes": 2}]
}
```

`users` is populated only for hub nodes collected at `full`.
A node outside the caller's scope returns `not_found`.

### `GET /api/series`

The time-series endpoint behind every chart.

| Parameter | Default | Meaning |
|---|---|---|
| `kind` | required | `inbound`, `outbound`, `user` or `online`. |
| `from`, `to` | last 24h | Unix seconds. |
| `step` | auto | Bucket width in seconds, minimum 60. |
| `node` | all | Node key filter. |
| `fleet` | scope | Fleet filter, within the caller's scope. |
| `type` | all | `hub` or `exit`. |
| `entity` | all | Inbound/outbound tag, or user email. |
| `dir` | both | `up` or `down`. |
| `agg` | `none` | `none` (per node, entity and dir), `entity` (collapse nodes), `node` (collapse entities), `total` (per dir only). |

```json
{"from": 1721654400, "to": 1721740800, "step": 300,
 "series": [{"node": "main/hub01", "entity": "vless-in", "dir": "down", "points": [0, 1024, 2048]}]}
```

Points are dense and aligned to `from + i*step`.
The returned `from` is the requested one floored to a step boundary; `to` is echoed as asked, and buckets are whole steps, so the last point can cover a little past it.

Omitting `step` picks the smallest of 60, 300, 900, 3600, 21600 or 86400 seconds that keeps the result under 400 points.
Minute buckets are read below a 3600-second step, hour buckets at or above it.
More than 2000 points is rejected with `bad_query`.

`kind=online` returns gauge counts of online users rather than byte totals; only the `node` and `fleet` filters apply to it.

### `GET /api/users`

Per-user traffic, hub nodes only.
Parameters: `range`, `seen`, `fleet`.

```json
[{
  "email": "alice@example",
  "up_bytes": 1, "down_bytes": 2,
  "online_now": true,
  "hubs": [{"node": "main/hub01", "last_seen": 1721740780}]
}]
```

`hubs` uses the `seen` window rather than the traffic range.

### `GET /api/users/{email}`

One identity in detail.
Parameters: `range`, `seen`.

```json
{
  "email": "alice@example",
  "up_bytes": 1, "down_bytes": 2,
  "online_now": true,
  "nodes": [{"node": "main/hub01", "up_bytes": 1, "down_bytes": 2}],
  "seen_hubs": [{"node": "main/hub01", "last_seen": 1721740780}],
  "ips": [{"node": "main/hub01", "ip": "203.0.113.9", "last_seen": 1721740780}]
}
```

`nodes` is traffic over the range, `seen_hubs` is presence over the `seen` window, and `ips` is the current snapshot with IP-less entries dropped.

### `GET /api/online`

Current online sessions, one row per identity per node.
Parameters: `fleet`.

```json
[{"node": "main/hub01", "email": "alice@example",
  "ips": [{"node": "main/hub01", "ip": "203.0.113.9", "last_seen": 1721740780}]}]
```

A snapshot refreshed each poll, not history.
Only nodes collected at `full` contribute.

## Prometheus

`GET /metrics` serves Prometheus text exposition when `metrics.enabled` is true, behind the same bearer token as the API and scoped the same way.

| Metric | Type | Labels |
|---|---|---|
| `orrery_traffic_bytes_total` | counter | `node`, `kind`, `entity`, `dir` |
| `orrery_node_up` | gauge | `node`, `fleet`, `type` |
| `orrery_node_last_ok_timestamp_seconds` | gauge | `node` |
| `orrery_xray_uptime_seconds` | gauge | `node` |
| `orrery_xray_goroutines` | gauge | `node` |
| `orrery_xray_alloc_bytes` | gauge | `node` |
| `orrery_xray_sys_bytes` | gauge | `node` |

Traffic is exported cumulative, so `rate()` and `increase()` behave normally.

Scraping needs a token like any other client:

```yaml
scrape_configs:
  - job_name: orrery
    authorization: { credentials: "<collector token>" }
    static_configs: [{ targets: ["127.0.0.1:9800"] }]
```
