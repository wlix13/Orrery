---
title: Deployment and operations
description: Building, deploying and running the Orrery collector, and the day-2 questions that follow.
---


Practical questions, deployment-first.
[Getting started](/Orrery/guides/getting-started/) is the shorter path for a first collector; this page is the production shape and the day-2 questions.

## Collector

### What must be true before Orrery can collect anything?

Every node's Xray config needs the `stats`, `api` and `policy` blocks described in [Xray stats configuration](/Orrery/guides/xray-stats/).
Without them, `orrery probe` connects but returns zero counters.

### How do I install it?

Take the binary from a release rather than building one:

```bash
# on the collector host, in an empty directory
curl -fsSLO https://github.com/wlix13/Orrery/releases/latest/download/orrery-linux-amd64
curl -fsSLO https://github.com/wlix13/Orrery/releases/latest/download/SHA256SUMS
sha256sum --ignore-missing --check SHA256SUMS
sudo install -m 0755 orrery-linux-amd64 /usr/local/bin/orrery
sudo useradd --system --home /var/lib/orrery --shell /usr/sbin/nologin orrery
sudo mkdir -p /etc/orrery /var/lib/orrery && sudo chown orrery: /var/lib/orrery
sudo cp orrery.yaml /etc/orrery/orrery.yaml       # start from orrery.example.yaml
echo 'ORRERY_TOKEN=<random long token>' | sudo tee /etc/orrery/env >/dev/null
sudo chmod 600 /etc/orrery/env
```

Assets are `orrery-linux-{amd64,arm64}`, each with a `-nodashboard` variant, plus `SHA256SUMS`.
`SHA256SUMS` covers every asset in the release, so `--ignore-missing` checks the one you downloaded.
Swap `latest/download` for `download/v0.1.0` to pin a version.
Generate the token with `openssl rand -hex 32`.

### How do I run it as a service?

Orrery ships no unit file.
It is one static binary that reads one config path and writes one database, so run it under whatever supervisor you already use.

A systemd unit to adapt, if that is your supervisor - the user, paths and hardening are all yours to choose:

```ini
[Unit]
Description=Orrery - Xray metrics collector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=orrery
Group=orrery
ExecStart=/usr/local/bin/orrery -config /etc/orrery/orrery.yaml serve
Restart=on-failure
RestartSec=5s
EnvironmentFile=-/etc/orrery/env
StateDirectory=orrery
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/var/lib/orrery
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

`EnvironmentFile` is what feeds `${ORRERY_TOKEN}` into the config's `auth.tokens`.

### What does the SSH setup need?

Orrery needs its own account on each node.
It only opens a loopback port-forward to the Xray stats API, so it should be a forward-only login: `nologin` shell, no sudo, and an authorized key restricted with `permitopen`.

Generate the key on the collector host, as the user the collector runs as:

```bash
sudo install -d -m 0700 -o orrery -g orrery /var/lib/orrery/.ssh
sudo -u orrery ssh-keygen -t ed25519 -f /var/lib/orrery/.ssh/orrery_ed25519 -N ''
```

Then install the public key on every node, restricted to the stats port:

```text
restrict,permitopen="127.0.0.1:10085" ssh-ed25519 AAAA... orrery@collector
```

`restrict` disables agent and X11 forwarding, PTY allocation and command execution; `permitopen` limits the tunnel to the one port Orrery reads.
Point `fleets[].ssh.user` and `fleets[].ssh.key_file` at that account and key.

The account, its key and the `permitopen` restriction are the only requirements, so any configuration-management tool can provision them across a fleet.

### How does Orrery verify node host keys?

Global `host_key_verify` setting, one of:

- **`sshfp`** (recommended) - matches the node's SSH host key fingerprint against its **DNS SSHFP records**. No `known_hosts` to maintain, and new nodes are covered as soon as they publish SSHFP. Keep `sshfp_require_dnssec: true`, since SSHFP without DNSSEC is spoofable; it needs a validating resolver on the collector host.
- **`known_hosts`** - matches `fleets[].ssh.known_hosts`. Populate it with `ssh-keyscan` over the node list.
- **`insecure`** - accept any key. Test only.

### When should I use `dial: direct` instead of `ssh`?

`direct` skips SSH: Xray binds its API on a routable address and the node firewall allows only the collector's addresses to that port.
Simpler runtime, but the gRPC is **plaintext on the internet** - fine for tag-level exit traffic, think twice for hubs where per-user emails and IPs cross the wire.
`ssh` (default) needs no firewall changes and is encrypted; use it unless you have a reason not to.

### Where is the data, and how do I back it up?

SQLite file at `db:` (deployment default `/var/lib/orrery/orrery.db`).
Backup while running: `sqlite3 /var/lib/orrery/orrery.db ".backup /tmp/orrery-backup.db"`.
Retention is config: minute buckets 72h, hour buckets 90d by default.

Prefer MongoDB? Set `db: "mongodb://user:pass@host:27017/orrery"` - same API, same dashboard, no other changes.
SQLite remains the default and the recommendation for a single-host deployment.

### Several fleets in one collector?

Yes - one `fleets:` entry each, pointing at their respective `topology.yaml` files.
Node keys are namespaced (`main/hub01`, `other/exit01`), and the dashboard's fleet filter separates them.

### How do I give one team access to only its fleet?

Each entry under `auth.tokens` is a principal with its own scope:

```yaml
auth:
  tokens:
    - name: ops                      # no fleets: every fleet
      token: "${ORRERY_TOKEN}"
    - name: caas-team
      token: "${ORRERY_CAAS_TOKEN}"
      fleets: [caas]
```

That token sees only `caas` everywhere: node lists, users, online sessions, charts, `/metrics` and the Overview.
Totals and top-10 lists are computed inside the scope, so they are that fleet's real numbers rather than a filtered view of everything.

The dashboard needs no configuration for this: a single-fleet token simply gets the single-fleet layout.

### How do I add or remove a node?

Edit the fleet's topology file, deploy the node as usual, then restart the collector.
Nodes are re-registered at startup; removed nodes disappear from the API, and their history rows stay in the database but are no longer reachable.
Nodes outside the topology go under `fleets[].nodes` with an `address` and `type`.

### What happens when things restart?

- **Xray restarts**: counters reset to zero. Orrery books the post-restart value as the delta, so nothing is double-counted.
- **Orrery restarts**: the delta base is persisted per poll, so it resumes without double counting. Traffic during its downtime is attributed to the first poll after it returns.
- **Node unreachable**: status goes `stale` (>2 poll intervals) then `down` (>5).

## Dashboard

### Self-hosted - how do I open it?

It's already inside the binary, unless that binary is a `-nodashboard` build.
Browse to the collector: `http://127.0.0.1:9800`.
On first load a token gate appears - paste one of the collector's `auth.tokens` values; it's stored in the browser's localStorage.

The collector binds loopback by default.
To reach it from elsewhere, put it behind a Cloudflare Tunnel or a reverse proxy, or SSH-forward it for personal use: `ssh -L 9800:127.0.0.1:9800 collector-host`.

### Cloudflare Workers - how do I deploy and connect it?

The Worker serves the same SPA build and proxies `/api/*` to the collector, holding the collector token server-side:

```bash
cd dashboard && pnpm install && pnpm build
cd worker
npx wrangler secret put DASHBOARD_TOKEN   # what browsers must present
npx wrangler secret put COLLECTOR_TOKEN   # one of the collector's auth.tokens
# set COLLECTOR_URL in wrangler.jsonc vars, e.g. https://orrery.<your-domain>
npx wrangler deploy
```

The collector's API must be reachable **from Cloudflare's network**, since the Worker calls it over HTTPS.
Browsers log in with `DASHBOARD_TOKEN`; the collector token never leaves the Worker.

### How do I put Cloudflare Access in front of the dashboard?

Put it in front of the Worker, which is where Orrery verifies it.
The collector itself only checks bearer tokens, so a self-hosted collector needs no Access configuration.

Create an Access application for the Worker's hostname, then hand the Worker the same identity:

```bash
npx wrangler vars set ACCESS_TEAM_DOMAIN "yourteam.cloudflareaccess.com"
npx wrangler vars set ACCESS_AUD "<the application's AUD tag>"
```

The Worker verifies the Access assertion itself against your team's public keys.
Visitors then need no token, and unsetting `DASHBOARD_TOKEN` makes Access the only way in.

To show different people different fleets, give each identity its own collector token:

```bash
npx wrangler secret put COLLECTOR_TOKENS
# {"ops@example.com": "<ORRERY_TOKEN>", "caas@example.com": "<ORRERY_CAAS_TOKEN>"}
```

Scope those tokens in `orrery.yaml` as above; the collector enforces the scope, so the mapping picks between scopes rather than widening one.
`COLLECTOR_TOKEN` is the fallback for identities the map does not name - leave it unset if only mapped identities should get through.

### Can I try the dashboard without a collector?

Yes - enter `mock` as the token (or run `VITE_MOCK=1 pnpm dev` locally).
It renders against generated data: two fleets, eight nodes, twelve users.

### Can Grafana consume this instead?

Yes - enable `metrics: { enabled: true }` and scrape `/metrics` with Prometheus (bearer token auth), e.g.:

```yaml
scrape_configs:
  - job_name: orrery
    authorization: { credentials: <collector token> }
    static_configs: [{ targets: ["127.0.0.1:9800"] }]
```

Counters are cumulative, so `rate()` and `increase()` behave normally.
The exported metrics are listed in the [API reference](/Orrery/reference/api/#prometheus).

## Reference

| Port | What | Where |
|---|---|---|
| 9800 | Collector API + dashboard | collector loopback (default) |
| 10085 | Xray StatsService gRPC | Node loopback (`ssh` dial) or firewalled public (`direct`) |

| Path | What |
|---|---|
| `/etc/orrery/orrery.yaml` | Config (fleets, ssh, retention, token env ref) |
| `/etc/orrery/env` | `ORRERY_TOKEN=...` (systemd EnvironmentFile) |
| `/var/lib/orrery/orrery.db` | SQLite data (unless `db:` is a mongodb:// URI) |
