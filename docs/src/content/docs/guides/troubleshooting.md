---
title: Troubleshooting
description: What the common failures look like and which layer to fix.
---

Start with `probe`.
It uses the same config and the same dial path as the poller and writes nothing, so it separates a node problem from a collector problem:

```bash
orrery -config /etc/orrery/orrery.yaml probe main/hub01
```

If `probe` works, `serve` will reach that node too.

## The node connects but reports nothing

### `0 counters`

The SSH login worked, the gRPC call worked, and Xray answered that it is counting nothing.

Add the `stats`, `api` and `policy` blocks from [Xray stats configuration](/Orrery/guides/xray-stats/) and restart Xray.

### Counters exist but there are no users

Per-user counters need `statsUserUplink` and `statsUserDownlink` under `policy.levels."0"`.
Beyond that, three deliberate behaviours look like missing data:

- Only **hub** nodes report per-user data. Exit "users" are per-hub pseudo-identities, so the API scopes user queries to hubs.
- The node's collect level must be `full`. Exits default to `traffic`, which is tags and sys stats only.
- Clients are level 0 unless configured otherwise, so a policy covering only another level counts nobody.

### Online users are always empty

`statsUserOnline` must be on and the node must be collected at `full`.
Older Xray builds answer `Unimplemented` for the online-user calls; Orrery falls back and then degrades gracefully, so traffic keeps flowing while presence stays empty.

## The node does not connect

`/api/nodes` only labels the failure (`node unreachable`); the errors below are in the collector's log, and `orrery probe <fleet>/<id>` reproduces them in the terminal.

### `ssh handshake ...: permission denied (publickey)`

The key is not installed for that user on that node, or `fleets[].ssh.user` names a different account than the one holding the key.

### `ssh tunnel to 127.0.0.1:10085: administratively prohibited: open failed`

SSH authenticated, then refused the forward.
The `permitopen` option in `authorized_keys` allows one host and port, and the request did not match it.

Usually the ports disagree: `permitopen="127.0.0.1:10085"` against a different `xray_api_port`, or against an Xray `api.listen` on another port.
All three have to name the same number.

### `ssh tunnel ...: connect failed`

SSH is fine and the forward is allowed, but nothing is listening inside the node.
Xray has no `api.listen`, is bound to a different address, or is not running.
`ss -lntp | grep 10085` on the node settles it.

### `knownhosts: key mismatch` or `key is unknown`

Either the host key changed, or the node was never recorded.

With `host_key_verify: known_hosts`, add it: `ssh-keyscan <host> >> <the file named in fleets[].ssh.known_hosts>`.
Go's `knownhosts` matching is case-sensitive where OpenSSH is not, so Orrery lower-cases hostnames before lookup; a mixed-case entry written by hand may not match.

### `no SSHFP records published for <host>`

`host_key_verify: sshfp` and the node has no SSHFP records in DNS.
Publish them, or move that fleet to `known_hosts`.

### `sshfp for <host> is not DNSSEC-authenticated`

The records exist but the resolver did not set the AD bit.
Point the collector host at a validating resolver.
`sshfp_require_dnssec: false` disables the check and the protection with it.

### `host key for <host> (SHA256:...) matches no SSHFP record`

The records validate, but none matches the key the node presented.
Either the host key was rotated without updating DNS, or something is answering in its place.

## The collector will not start

Config errors are reported together, so fix the whole list.

- **`fleet "x": needs a topology file or explicit nodes`** - a fleet has to get its nodes from somewhere.
- **`fleet "x" node "y": type is required without a topology file`** - inline nodes must declare `hub` or `exit`.
- **`fleet "x" node "y": no address`** - a node absent from topology needs an explicit `address`.
- **Unknown fleet in a token scope** - `auth.tokens[].fleets` may only name fleets that exist.
- **`unknown node "hub01" (known: use <fleet>/<id>)`** - `probe` takes the full node key, `main/hub01`.

## The API says no

- **401 with `unauthorized`** - no `Authorization: Bearer` header, or a token that matches nothing.
- **Empty results for a fleet you know exists** - the caller's token is scoped elsewhere. `GET /api/me` shows its actual scope.
- **404 with `not_found` at `/`** - the binary was built with `-tags nodashboard`, carries no SPA, or has `dashboard.enabled: false`.
- **400 with `bad_query` on a chart** - the range and step combination asks for more than 2000 points. Widen the step or narrow the range.

## The numbers look wrong

### A gap in the history

Traffic that happened while the collector was down is attributed to the first poll after it returns, so a restart shows as one tall bucket rather than a hole.

### A node's traffic jumped after an Xray restart

Counters live in Xray's memory and reset to zero when it restarts.
Orrery notices the value going backwards and books the current value as the delta, so nothing is double-counted.
The traffic between the last poll and the restart is lost, since Xray no longer knows about it.

### Old data disappeared

Retention.
Minute buckets are kept for `retention.minute` (72h by default) and hour buckets for `retention.hour` (90 days).
Charts pick the resolution that fits the requested range, so a 30-day chart keeps working after the minute data behind it is gone.

### A node shows as `off`

Its collect level is `off`.
It is registered and visible but never polled.

## Checking the collector itself

```bash
curl -s localhost:9800/healthz                      # status, version, uptime - no token
curl -s -H "Authorization: Bearer $ORRERY_TOKEN" localhost:9800/api/me
curl -s -H "Authorization: Bearer $ORRERY_TOKEN" localhost:9800/api/nodes | jq '.[] | {node, status, last_err}'
```

Poll failures are logged with the node key and the underlying error.
