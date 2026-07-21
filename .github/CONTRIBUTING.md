# Contribution Guidelines

First of all, thank you for your interest in the project.

Regardless of whether you are thinking about creating an issue or opening a pull request, I truly appreciate the help.
Without communication, I cannot know what the community wants, what they use or how they use it.
Pull requests and issues are exactly what gives me the visibility I need, so thank you.

## Getting started

While following the exact process is neither mandatory nor enforced, it is recommended to try following it as it may help avoid wasted efforts.

### Creating an issue

Whether you have a **question**, found a **bug**, would like to see a **new feature** added or anything along those lines, creating an issue is going to be the first step.
The issue templates will help guide you, but simply creating an issue with the reason for the creation of said issue is perfectly fine.

Furthermore, if you don't want to fix the problem or implement the feature yourself, that's completely fine.
Creating an issue alone will give both the maintainers as well as the other members of the community visibility on said issue, which is a lot more likely to get the issue resolved than if the problem/request was left untold.

### Solving an issue

Looking to contribute? Awesome! Look through the open issues in the repository, preferably those that are already labelled.

If you found one that interests you, try to make sure nobody's already working on it.
Adding a comment to the issue asking the maintainer if you can tackle it is a perfectly acceptable way of doing that!

If there's no issue yet for what you want to solve, start by [Creating an issue](#creating-an-issue), specify you'd like to take a shot at solving it, and finally, wait for the maintainer to comment on the issue.

You don't _have_ to wait for the maintainer to comment on the issue before starting to work on it if you're sure that there's no other similar existing issues, open or closed, but if the maintainer has commented, it means the maintainer has, based on the comment itself, acknowledged the issue.

## Development Setup

### Prerequisites

- **Go** as pinned in `collector/go.mod`. The collector is CGO-free, so nothing else is needed to build it.
- **Node 22** and **pnpm 11** for the dashboard, the Cloudflare Worker and the documentation site.
- **Docker** (optional). The MongoDB store tests spin up a `mongo:8` container and skip cleanly when Docker is absent, so running without it silently reduces coverage.

### Task

Every check, build and release step in this project goes through [Task](https://taskfile.dev), and CI runs the exact same tasks you do.
Install it with `brew install go-task` or `go install github.com/go-task/task/v3/cmd/task@latest`, or run it without installing:

```bash
go run github.com/go-task/task/v3/cmd/task@latest <task>
```

`task` on its own lists everything available. The ones you need day to day:

```bash
task ci            # everything CI checks, in one run: lint + tests + typechecks
task lint          # golangci-lint (both build variants) + betteralign
task lint:fix      # apply lint autofixes and struct realignment
task test          # test:go + typecheck + test:worker
task test:go       # go test ./... + the nodashboard build and vet
task typecheck     # dashboard and Worker TypeScript
task test:worker   # Worker unit tests
task all           # build the dashboard, embed it, build ./dist/orrery
task build:nodashboard   # collector-only binary, no embedded SPA
```

Run `task ci` before pushing.
It is the same set of checks the pull request will run, just serially instead of across parallel jobs.

### Working on the dashboard

The dashboard runs against generated mock data, so you do not need a collector or a fleet to work on it:

```bash
cd dashboard && VITE_MOCK=1 pnpm dev   # the token is "mock"
```

The SPA is embedded into the binary by `task all`, which copies the build into `collector/internal/api/webui/dist`.
Do not commit that output.

### Working on the collector

`orrery probe <fleet>/<id>` dials a single node over the same path the poller uses and prints what it gets back, which is the fastest way to check a change against a real node:

```bash
./dist/orrery -config orrery.yaml probe main/hub01
```

`orrery.example.yaml` documents every setting.

## Commits

All commits are expected to follow the conventional commits specification.

```text
<type>[scope]: <description>
```

This one **is** a big deal here: release-please derives the next version and the changelog straight from commit subjects, so a malformed subject is a malformed release.
Never tag a release by hand.

The project is pre-1.0 with `bump-patch-for-minor-pre-major`, so `feat:` is a patch bump and `feat!:` a minor one.

Here's a few examples of good commit messages:

- `feat(collector): add online users to the node API`
- `fix(collector): keep deltas correct across an Xray restart`
- `test(store): cover fleet scope isolation in both backends`
- `docs: add a paragraph on running the collector locally`

## Pull requests

The **pull request title** should be a short, meaningful summary of the change as a whole, written in the imperative mood.
GitHub pre-fills it with your first commit message, so please replace that with something descriptive. Examples:

- `Add online users to the node API`
- `Improve CI/CD caching and job layout`
- `Fix double-counted traffic after an Xray restart`

A good title reads cleanly in the PR list.
The individual **commits** inside the PR still follow Conventional Commits (see [Commits](#commits)); because PRs are merged with a merge/rebase strategy, those commit messages, not the title, drive the release changelog, so the title itself does not need a `type:` prefix.

PRs are labelled automatically from the files they touch (e.g. `store`, `poller`, `api`, `dashboard`, `ci-cd`, `docs`) and from the **Type of change** checklist in the PR template.
Tick the boxes that apply and the matching `feature` / `bug` / `enhancement` / `refactor` / `breaking-change` / `security` label is added.
Dependabot PRs are labelled `dependencies`.
