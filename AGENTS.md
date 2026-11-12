# Mentat

Mentat (formerly Stack Explorer) is a local research platform for the AI agent and developer-tooling
ecosystem. It keeps a SQLite catalog of repositories, scores them through weighted
lenses, extracts symbols and code relationships, and serves the result over a CLI,
a REST API and an MCP endpoint. It does not perform the scans itself — quantitative
analysis comes from Hadron blueprints — and it is not a task tracker, an agent
launcher, or a durable store for project knowledge.

## Start Here

- `cmd/mentat/` is the CLI surface: one `cmd_<group>.go` per command group.
- `internal/persistence/migrations/sqlite/` is the schema source of truth — goose
  SQL, embedded and applied at start. `cmd/mentat/substrate.yaml` is the manifest
  (regenerate the wiring with the substrate CLI; `docs/conversion-notes.md`).
- `internal/api/` is the REST handler set on `net/http` (`serve`); `server.go` owns
  routes, their scopes, pagination and the sort whitelist.
- `internal/mcp/` is the MCP tool surface (served at `/mcp` and by `mentat mcp`);
  `docs/mcp-tools.md` is the tool reference.
- `internal/retrieval/` fuses FTS5 BM25 with vector search over `internal/embed/`;
  `internal/symbols/` and `internal/graph/` produce what it retrieves.
- `docs/product-vision.md` is the data model and direction; `docs/audit-format.md`
  defines the deep-review ingest format.
- `blueprints/` holds the Hadron blueprints; `scripts/` holds the batch scan,
  ingest, seed and export shells.

## Commands

```bash
make build          # -> ./mentat, pure Go
make test
make lint           # go vet + golangci-lint v2.11.4 (the CI gate)
make build-full     # adds the treesitter tag — the only CGo path here
```

`make eval` rewrites the tracked `eval/baseline.json`. Run it only when you intend
to move the retrieval baseline, and commit that diff deliberately.

## Boundaries

The default build is pure Go on `modernc.org/sqlite`, with no CGo. Tree-sitter
symbol extraction sits behind the `treesitter` build tag
(`internal/symbols/factory_full.go`), so `make test` neither compiles nor exercises
it — use `go test -tags treesitter ./internal/symbols/...` when you change it.

The live database is in the user data directory (`~/.local/share/mentat`) and runs the
deployed service. Never point a test, a tool or a manual run at it: every test sets an
explicit temp data directory (`MENTAT_DATA_DIR` or XDG), and `TestMain` in `cmd/mentat`
fails if the default resolves outside it. `scripts/export-state.sh` is the supported way
to move data.

`allowedSort()` in `internal/api/server.go` whitelists ORDER BY columns. Sort
parameters arrive from query strings, so every list handler routes them through it.

**The LongMemEval benchmark work is not on this branch.** It lives on the
`feature/longmemeval-vanta` branch, which has diverged from `main` in both
directions. Its `benchmarks/longmemeval/` tree does not exist here; do not
recreate it from planning documents that describe that branch.

The `local_path` entries in `repos.example.yaml` are examples; real checkout
paths are machine-specific and belong in your gitignored `repos.yaml`. A path
named in a document is not evidence the path is there.

`mentat serve` binds loopback only, refuses unknown `Host` headers, and every route
declares a scope (`TestEveryRouteHasItsScope`). Keep that; do not add an unscoped route.
