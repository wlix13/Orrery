# Orrery dashboard

TypeScript SPA for the Orrery metrics collector.
Vite + React + TS + Tailwind CSS v4, uPlot for charts.
No SSR; a ~50-line history-based router (`src/lib/router.tsx`) covers the app's 6 routes.

## Develop

```sh
pnpm install
pnpm dev
```

By default the SPA talks to a real collector - enter its API base URL and bearer token in the token-gate screen (or Settings later).
To develop against generated data instead of a running collector, either:

- enter `mock` as the token in the token-gate screen, or
- run with the mock backend forced on regardless of stored token:

  ```sh
  VITE_MOCK=1 pnpm dev
  ```

Mock mode (`src/api/mock.ts`) fabricates 2 fleets, 8 nodes and 14 users with sine-ish daily traffic waves + noise, so every page has plausible, internally-consistent data without a backend.
Each fleet runs its own user namespace, and two identities are guests in another user's, which is what the identity rendering keys off.

Single-fleet deployments get a trimmed UI: no `fleet/` prefix on node keys, no fleet filter, and the Overview chart splits by direction instead of by fleet.
To see that shape against the mock fixtures:

```sh
VITE_MOCK=1 VITE_MOCK_SINGLE_FLEET=1 pnpm dev
```

## Build

```sh
pnpm build       # tsc -b && vite build -> dist/
pnpm typecheck   # tsc -b --force, typecheck only
```

`dist/` is a single static bundle, built once and deployed two ways:

## Self-hosted (embedded in the collector)

Copy `dist/` into `collector/internal/api/webui/` for `go:embed`.
The Go server serves SPA-fallback to `index.html`, hashed assets under `assets/` `immutable`, and `index.html` itself `no-cache`.
The SPA calls `/api/*` on its own origin by default (leave the API base URL empty in Settings / the token gate), and sends the collector's own token as the bearer token.

Builds made with `-tags nodashboard` carry no SPA at all, and `dashboard.enabled: false` turns serving off at runtime.

## Cloudflare Workers

See `worker/README.md`.
Short version: `pnpm build` here, then from `worker/`, set the `DASHBOARD_TOKEN` / `COLLECTOR_TOKEN` secrets and `COLLECTOR_URL` var, and `npx wrangler deploy`.
The Worker serves the built assets and proxies `/api/*` to the collector, swapping in the collector's token so it never reaches the browser.
