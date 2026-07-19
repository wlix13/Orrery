# Orrery dashboard - Cloudflare Worker

Static-asset host + thin auth proxy for the SPA in `dashboard/`.
Serves the built `dashboard/dist` and proxies `/api/*` to the collector, swapping the visitor's credential for a collector token so the latter never reaches the browser.

## One-time setup

```sh
cd dashboard/worker
pnpm install   # or npm install

# Secrets (never stored in wrangler.jsonc):
npx wrangler secret put DASHBOARD_TOKEN   # token the browser/SPA must send
npx wrangler secret put COLLECTOR_TOKEN   # token the collector expects

# Non-secret config:
npx wrangler vars set COLLECTOR_URL "https://collector.example.internal:9800"
# (or edit the `vars.COLLECTOR_URL` placeholder in wrangler.jsonc directly)
```

`DASHBOARD_TOKEN` is whatever you hand out to dashboard users - it can differ from the token the collector expects.
`COLLECTOR_URL` should be reachable from Cloudflare's network (direct HTTPS, or a Cloudflare Tunnel hostname).

## Cloudflare Access

Access is the better front door here: visitors sign in with your identity provider instead of pasting a shared token.
Create an Access application for the Worker's hostname, then point the Worker at it:

```sh
npx wrangler vars set ACCESS_TEAM_DOMAIN "yourteam.cloudflareaccess.com"
npx wrangler vars set ACCESS_AUD "<the application's AUD tag>"
```

The Worker verifies the `Cf-Access-Jwt-Assertion` header against your team's public keys - signature, issuer, audience and expiry - rather than trusting the `Cf-Access-*` headers, so a route Access does not front (the `workers.dev` URL, say) is not a way in.
`DASHBOARD_TOKEN` keeps working alongside Access; unset it to make Access the only way in.

To give identities different fleets, give each one its own collector token:

```sh
npx wrangler secret put COLLECTOR_TOKENS
# {"ops@example.com": "<token scoped to all fleets>", "eu@example.com": "<token scoped to eu>"}
```

Scope the matching `auth.tokens` entries in `orrery.yaml`; the collector enforces the scope, so nothing here can widen it.
`COLLECTOR_TOKEN` is the fallback for identities the map does not name - leave it unset if only mapped identities should get through.

## Collector side

With the dashboard living here, the collector doesn't need to serve one.
Build the collector-only binary; it carries no SPA and compiles without this dashboard having been built at all:

```sh
task build:nodashboard        # -> dist/orrery-nodashboard
# task release also emits dist/orrery-linux-{amd64,arm64}-nodashboard
```

An ordinary build works too: set `dashboard.enabled: false` in `orrery.yaml` to stop it serving the SPA.
Either way `/` returns a JSON 404 and `/api/*` is unaffected.

**The collector still needs a credential configured** even when it listens on loopback behind a Tunnel.
cloudflared republishes that port to the internet, and `/metrics` sits behind the same auth as `/api/*`.
Put one of its `auth.tokens` values in `COLLECTOR_TOKEN` here; scope that token with `fleets` if this dashboard should only show part of the estate.

The collector knows nothing about Access: this Worker terminates it, strips the visitor's `Cf-Access-*` headers, and speaks to the collector with a bearer token like any other client.

## Build + deploy

```sh
# from dashboard/
pnpm build              # produces dashboard/dist

cd worker
npx wrangler deploy
```

Re-run `pnpm build` + `wrangler deploy` whenever the dashboard changes; `wrangler deploy` re-uploads whatever is currently in `../dist`.

## Local dev

```sh
npx wrangler dev
```

Requires `../dist` to already exist (`pnpm build` in the parent directory first) and secrets/vars to be set.
wrangler dev reads them the same way as deploy, or pass `--var COLLECTOR_URL:...` / `.dev.vars` for local-only overrides - see `.dev.vars` in the wrangler docs.

## Validating config without deploying

```sh
npx wrangler deploy --dry-run
```
