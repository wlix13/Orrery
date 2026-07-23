# Orrery

![Go](https://img.shields.io/github/go-mod/go-version/wlix13/Orrery?filename=collector%2Fgo.mod&logo=go&logoColor=white)
![Build](https://img.shields.io/github/actions/workflow/status/wlix13/Orrery/ci-tests.yaml?label=build&logo=github)
![Lint](https://img.shields.io/github/actions/workflow/status/wlix13/Orrery/ci-code-quality.yaml?label=lint&logo=github)
![Release](https://img.shields.io/github/v/release/wlix13/Orrery?logo=github)
![License](https://img.shields.io/badge/license-MIT-green)
![Task](https://img.shields.io/badge/build-Task-29beb0?logo=task&logoColor=white)
[![Docs](https://img.shields.io/badge/docs-GitHub%20Pages-0075ca?logo=astro&logoColor=white)](https://wlix13.github.io/Orrery/)

Metrics collection and dashboard for the **Conglomerate** proxy fleet.
Polls every node's Xray `StatsService` over gRPC (through SSH tunnels or direct), stores traffic history in SQLite, and serves a JSON API, a web dashboard, and an optional Prometheus endpoint - all from one binary.

An orrery is a mechanical model of the solar system; this one watches hubs (*perigee*) and exits (*aphelion*).

```text
nodes (xray gRPC) ──ssh/direct──► orrery collector ──► SQLite
                                     │
                     ┌───────────────┼──────────────────┐
                 JSON API      embedded dashboard   /metrics (Prometheus)
                                     │
                       optional: Cloudflare Workers dashboard
```

Full documentation: **<https://wlix13.github.io/Orrery/>**

[Architecture and the API contract](https://wlix13.github.io/Orrery/reference/architecture/), [deployment and day-2 operations](https://wlix13.github.io/Orrery/guides/deployment/), [every config setting](https://wlix13.github.io/Orrery/reference/configuration/).

## Layout

| Path | What |
|---|---|
| `collector/` | Go collector: poller, SQLite store, HTTP API, embedded SPA |
| `dashboard/` | Vite + React + Tailwind dashboard (uPlot charts) |
| `dashboard/worker/` | Cloudflare Worker: serves the same SPA + API proxy |
| `docs/` | Astro + Starlight documentation site, published to GitHub Pages |

## Quick start

Prerequisites: nodes must expose Xray's stats API - see [Xray stats configuration](https://wlix13.github.io/Orrery/guides/xray-stats/).

```bash
task all                      # build SPA, embed it, build ./dist/orrery
cp orrery.example.yaml orrery.yaml  # edit: fleets, ssh key, token
./dist/orrery probe main/hub01   # smoke-test one node's connectivity
./dist/orrery serve
```

Tasks are run with [Task](https://taskfile.dev) (`brew install go-task`, or zero-install: `go run github.com/go-task/task/v3/cmd/task@latest <task>`).

- Hub nodes are collected in full (per-inbound tags, per-user traffic, online users); exit nodes default to tag-level traffic only. Both are per-node configurable (`collect: full|traffic|off`).
- Reads are non-destructive (`reset:false` always) - Orrery can coexist with any other stats consumer.
- Access is per-credential: each token is scoped to a set of fleets, and every query, aggregate and metric is computed inside that scope.
- Storage: embedded SQLite by default; set `db` to a `mongodb://` URI to use MongoDB instead (both backends pass the same conformance suite).
- `task release` cross-compiles static Linux binaries for the collector host; CI runs it on release and attaches the binaries plus `SHA256SUMS` to the GitHub release.
- Versioning is automated from Conventional Commits by release-please - no manual tagging.

## Dashboard hosting

Self-hosted: already embedded - `orrery serve` serves it at `/`.
Enter the collector token once in the dashboard's Settings gate.

Cloudflare Workers: deploys the same `dashboard/dist` with a thin proxy Worker that keeps the collector token server-side and can sign visitors in with Cloudflare Access.
See [dashboard/worker/README.md](dashboard/worker/README.md).

## Development

```bash
task ci                       # lint + tests + typechecks, as CI runs them
task test                     # go tests + typechecks + worker tests
task lint                     # lint:go + lint:md
task lint:go                  # golangci-lint (wsl_v5, fatcontext, gocognit + std) + betteralign
task lint:md                  # markdownlint-cli2 over every Markdown file
task lint:fix                 # apply autofixes, struct realignment and Markdown fixes
task docs                     # build the documentation site, as CI does for docs/ changes
task docs:dev                 # documentation site with live reload
cd dashboard && VITE_MOCK=1 pnpm dev   # dashboard against generated mock data
```

See [CONTRIBUTING.md](.github/CONTRIBUTING.md) for the full setup, commit conventions and pull request flow.
