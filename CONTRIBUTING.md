# Contributing to Lattice

Thanks for your interest! Lattice is a single Go binary (hub + agent) with an embedded
React/TypeScript dashboard.

## Prerequisites

- **Go** — see the version in [`go.mod`](go.mod).
- **Node.js** (18+) — for the dashboard.

## Build

`scripts/build.sh` bundles the dashboard, embeds it, and cross-compiles every target from one
machine (pure-Go SQLite means `CGO_ENABLED=0` cross-compiles cleanly — no per-OS toolchain):

```sh
bash scripts/build.sh        # → dist/lattice-<os>-<arch>[.exe]
```

To run locally without cross-compiling:

```sh
go run . hub                 # the controller + dashboard
go run . agent --hub localhost:7400 --token "$(cat ~/.lattice/.lattice-token)" --name dev
```

## Checks (CI enforces these — run them before opening a PR)

```sh
go build ./...
go vet ./...
go test ./...                # add -race for concurrency-touching changes
golangci-lint run ./...      # 0 issues expected

cd dashboard
npm ci
npx tsc -b
npm run build
npm run lint                 # 0 errors
```

Go code is `gofmt`-clean; match the surrounding style and comment density (the codebase favors
short "why" comments over restating the "what"). Architectural decisions are recorded in
[`docs/DECISIONS.md`](docs/DECISIONS.md) — skim it before a structural change.

## Pull requests

- Keep changes focused; one concern per PR.
- Make sure the checks above pass — CI runs build/vet/test, `golangci-lint`, and the dashboard
  `tsc`/build/lint as blocking jobs.
- Describe what changed and why; link any related issue.

## Security

Please **don't** file security vulnerabilities as public issues — see [`SECURITY.md`](SECURITY.md).
