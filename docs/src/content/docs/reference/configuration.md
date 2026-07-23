---
title: Configuration
description: Every setting the collector reads from orrery.yaml.
---

The collector reads a single YAML file, `-config orrery.yaml` by default.
A complete annotated example lives at [`orrery.example.yaml`](https://github.com/wlix13/Orrery/blob/main/orrery.example.yaml) in the repository; copy it and edit.

Values are env-expanded, so `${ORRERY_TOKEN}` picks the token up from the environment rather than baking a secret into the file.

## Server

| Setting | Default | Meaning |
|---|---|---|
| `listen` | `127.0.0.1:9800` | Address the HTTP API binds to. |
| `db` | `orrery.db` | SQLite file path, or a `mongodb://` / `mongodb+srv://` URI to select the MongoDB backend. |

## `auth`

Every read is scoped to the fleets its caller may see, so a credential is also an authorisation decision.
Either `auth.tokens` or `auth.allow_anonymous` is required, whatever `listen` is set to.

`/healthz` is the only route that never requires a credential.

### `auth.tokens`

| Setting | Default | Meaning |
|---|---|---|
| `name` | required | Identifies the principal in logs and in `/api/me`. |
| `token` | required | Bearer token. Env-expanded, so keep the secret out of the file. |
| `fleets` | all | Fleets this token may read. Naming a fleet that does not exist is a config error. |

Tokens are the collector's only credential.
Single sign-on belongs to whatever fronts it, such as the [Worker dashboard](/Orrery/guides/deployment/#how-do-i-put-cloudflare-access-in-front-of-the-dashboard).

### `auth.allow_anonymous`

Serves the API and `/metrics` to anyone who can reach the listener, and logs a warning on every start.
Mutually exclusive with tokens, which would never be checked.

### What scoping means

A scoped caller sees only its fleets in every response: node lists, users, online sessions, series, `/metrics` and the Overview.
Aggregates are computed inside the scope, so totals and top-10 lists are that caller's real numbers rather than truncated views of everything.
`?fleet=` narrows within the scope; a fleet outside it returns nothing.

The dashboard needs no configuration for this: a token scoped to one fleet gets the single-fleet layout.

## `poll`

| Setting | Default | Meaning |
|---|---|---|
| `poll.interval` | `60s` | Time between polls of each node. Start times are jittered. |
| `poll.timeout` | `15s` | Deadline for one node's poll, applied per node. |

## `retention`

| Setting | Default | Meaning |
|---|---|---|
| `retention.minute` | `72h` | How long minute-resolution buckets are kept. |
| `retention.hour` | `2160h` | How long hour-resolution buckets are kept. |

Both resolutions are written at ingest, so retention is a delete rather than a rollup job.

## `metrics` and `dashboard`

| Setting | Default | Meaning |
|---|---|---|
| `metrics.enabled` | `false` | Serve Prometheus text at `/metrics`, behind the same bearer token as the API. |
| `dashboard.enabled` | `true` | Serve the embedded SPA at `/`. |

`dashboard.enabled` is inert in binaries built with `-tags nodashboard`, which carry no SPA at all; setting it to `true` there logs a warning at startup and `/` returns a JSON 404.

## Host-key verification

| Setting | Default | Meaning |
|---|---|---|
| `host_key_verify` | `known_hosts` | `known_hosts`, `sshfp` or `insecure`. |
| `sshfp_require_dnssec` | `true` | With `sshfp`, require the resolver's AD bit before trusting a record. |

`insecure` accepts any host key and warns once at startup.
SSH hostnames are lower-cased before lookup, because Go's `knownhosts` is case-sensitive where OpenSSH is not.

## `fleets`

At least one fleet is required.
Each entry names a fleet and either points at a topology file listing its nodes or lists them inline.

| Setting | Default | Meaning |
|---|---|---|
| `name` | required | Fleet name; becomes the first half of every node key (`fleet/id`). |
| `topology` | none | Path to the fleet's topology file. Nodes may instead be listed inline. |
| `xray_api_port` | `10085` | Port Xray's `api.listen` uses inside each node. |
| `dial` | `ssh` | `ssh` tunnels gRPC to the node's loopback API; `direct` dials a routable address. |
| `ssh.user` | required for `ssh` | Login user, typically a dedicated forward-only account. |
| `ssh.key_file` | required for `ssh` | Private key path. |
| `ssh.port` | `22` | SSH port. |
| `ssh.known_hosts` | `~/.ssh/known_hosts` | Only consulted when `host_key_verify: known_hosts`. |
| `collect.hub` | `full` | Collection level for hub nodes. |
| `collect.exit` | `traffic` | Collection level for exit nodes. |
| `nodes[]` | none | Matched by `id`: overrides that topology node, or adds one when the id is unknown. Any of `address`, `type`, `region`, `dial`, `collect`, `xray_api_port`. |

Collection levels are `full` (tags, per-user traffic, online users, sys stats), `traffic` (tags and sys stats only) and `off`.
An `off` node is registered and shown in the UI but never polled, and reports as `off` rather than down.

### Nodes

A fleet needs a `topology` file or a non-empty `nodes` list, and may have both.
Only `regions[].{id,type}` and `nodes[].{id,hostname}` are read from the topology - see [the topology contract](/Orrery/reference/ecosystem/#the-topology-contract).

Entries in `nodes[]` are matched by `id`.
A matching id overrides that topology node; an unknown id adds one, and then two fields stop being optional:

- `type`, since there is no region to inherit `hub` or `exit` from.
- `address`, since it would otherwise default to the topology hostname.
