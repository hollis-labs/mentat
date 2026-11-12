# Architecture

Mentat is a Substrate application: one binary built from `cmd/mentat`, whose manifest
(`cmd/mentat/substrate.yaml`) names the modules and whose generated `substrate_wiring.go` starts them in order.

## Modules and what they own

| Module | Role here |
|---|---|
| `httphost` | The listener (`httphost.addr`, loopback only), `/healthz`, the handler chain. |
| `secrets`, `auth`, `identity` | Owner setup and sign-in, sessions, API keys and tokens, scope checks. Identity state lives in `identity_*` tables in the app database. |
| `database` | The SQLite store (one write connection plus a read-only pool) and the goose migration from `internal/persistence`. |
| `mcp` | The MCP server at `/mcp` and over stdio (`mentat mcp`). |
| `cli` | The root command, `config`, `health`, `version`. Mentat adds its command groups and `serve`, `mcp`, `migrate-legacy`. |

## Layers

- `cmd/mentat` is the command surface and the composition root: `main.go` boots the framework (`boot.StartWith`),
  resolves paths under the private data directory, refuses a legacy-layout database before the database module runs,
  and then builds the app's own services from the started modules.
- `internal/api` is the REST handler set on `net/http`. Every route is declared with its scope in `routes()`;
  `TestEveryRouteHasItsScope` fails for an unscoped route.
- `internal/mcp` registers the tools with `RegisterChecked`; each handler authorizes against the verified caller, and
  provenance (who wrote a finding) comes from that caller.
- `internal/store/db` is the facade over the framework store: reads use the pool, writes use the writer or `WriteTx`.
  Do not hold a cursor open while issuing another statement.
- `internal/store/sqlite`, `internal/audits`, `internal/retrieval`, `internal/graph`, `internal/symbols`,
  `internal/embed` are the domain code, unchanged in purpose.
- `internal/legacy` is the one-way copy of a pre-Substrate database into a new file (`mentat migrate-legacy`), with
  per-table digests, full-text-index and integrity proofs. `docs/cutover-plan.md` has the procedure and numbers.
- `internal/settings` decodes the manifest's `mentat:` block (allowed hosts, embedding profiles, actor); there are no
  other environment variables.
- `internal/appdirs` and `internal/hostguard` give the private data directory and the Host check.

## Where things run

`serve` runs the REST API, `/mcp` and the background scan worker. `mentat mcp` (stdio) and every other CLI command
run no background work. Tests never use the default data directory: `TestMain` redirects the XDG directories and
fails if the default resolves outside the test root.
