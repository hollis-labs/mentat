# Stack Explorer

Stack Explorer is a research and knowledge-retrieval platform for the AI
agent and developer-tooling ecosystem. It keeps a SQLite catalog of
repositories, scores them through weighted lenses, extracts symbols and code
relationships, and serves the result over a CLI, a REST API, and an MCP
endpoint. It does not scan code itself — quantitative analysis comes from
Hadron blueprints — and it is not a task tracker, an agent launcher, or a
durable store for project knowledge.

> **Pre-release.** Stack Explorer is pre-1.0 (tagged `0.x`), runs only in Hollis Labs'
> own fleet, and has no outside consumers. It's being built in the open: the code, the docs, and
> this README describe what exists today, not a pitch for what's planned.
> Interfaces and behavior change without notice, and there are no
> compatibility guarantees yet.

## What it is today

- **Repo catalog.** Repos → snapshots (time-series metrics) → scores
  (per-dimension, per-lens) → patterns → findings → tags. A repo scored once
  can be viewed through any lens (`agent-platform`, `chat-app`,
  `memory-system`, `agent-framework`, and others).
- **Audits as data.** Deep-review audits (Nanite's, first) are imported as
  queryable rows — `audit start|finish|list|show|import|export|diff` — rather
  than living as one-off markdown.
- **Hybrid retrieval.** `internal/retrieval/` fuses FTS5 BM25 with vector
  search over `internal/embed/`, so `search "panic recovery" --repo nanite`
  ranks findings and symbols together.
- **An MCP surface.** `internal/mcp/` exposes a read-heavy tool set —
  `repo_context`, `prior_art_for_file`, `prior_art_for_symbol`,
  `finding_search`, `symbol_lookup`, `audit_show`, `knowledge_query` — plus a
  few write tools for findings and audits, over stdio or HTTP. See
  [`docs/mcp-tools.md`](docs/mcp-tools.md).
- **A REST API.** `stack-explorer serve` runs the HTTP API over the same
  catalog, bound to `127.0.0.1` by default with optional bearer-token auth; `internal/jobs/` runs scheduled refresh work
  (`stack-explorer schedule`).

## Where it sits in the stack

```
   agents / sessions     Nanite, Claude Code, or any MCP-capable session
         │  MCP: repo_context, prior_art_for_file, finding_search, ...
    ┌────────────────┐
    │ Stack Explorer │   catalog + scoring + retrieval — read-heavy
    └────────────────┘
         │
   Hadron blueprints     quantitative scans that feed snapshots/scores
                          (Stack Explorer doesn't scan; Hadron does)
```

It's a peer to Tesseract and Torque, not a replacement for either: Stack
Explorer answers "what does the ecosystem look like / what did we already
find here," not "what's the plan" or "what did we decide."

## Examples

**Daily driver.** Before touching a file, pull prior art on it —
`prior_art_for_file(path="internal/chat/engine.go")` — to see findings and
audit history without re-deriving them. Comparing tools across the ecosystem
(leaderboards, gap analyses against a lens) is generated into `reports/` with
`stack-explorer report generate` and `stack-explorer scorecard generate`.

**Composition.** A Hadron blueprint scans a repo and produces the raw
snapshot/score data; Stack Explorer catalogs and ranks it. An agent session
(Nanite, Claude Code) calls Stack Explorer's MCP tools mid-session to pull
that context before editing code. When a gap surfaces that's worth acting on,
it becomes a Torque task or a Tesseract decision — Stack Explorer doesn't
track that itself.

## Roadmap

- **Comparative re-audits.** Diff a fresh audit against a prior one to see
  what changed, instead of re-auditing a repo from scratch. Nanite's
  deep-review audits are the first case.
- **Autonomous multi-phase audits.** Using the audit + diff surface as the
  proving ground for running staged, multi-step audit work through Torque
  unattended once it's tested.
- **Ecosystem signal enrichment.** GitHub activity (stars, releases,
  contributors), stack/framework cross-referencing, and trend tracking —
  directional per `docs/product-vision.md`, not scheduled.
- **Sigil-based GUI.** A lens-switchable leaderboard and drill-down dashboard
  on top of the existing CLI/API/MCP surfaces.

## License

MIT — see [LICENSE](LICENSE).

## Install

Requires Go 1.26.8+; the module selects the patched Go 1.26.9 toolchain.

```bash
go install github.com/hollis-labs/mentat/cmd/stack-explorer@latest
```

or build from source:

```bash
git clone https://github.com/hollis-labs/mentat.git
cd stack-explorer
make build          # -> ./stack-explorer, pure Go
make install        # or install to $GOBIN
```

`make build-full` adds the `treesitter` build tag for tree-sitter symbol
extraction — the only CGo path in the repo.

## Quick start

```bash
# Seed a catalog from the example manifest (public repos only)
./stack-explorer repo import repos.example.yaml
./stack-explorer repo list | head

# Lenses, scores, search
./stack-explorer lens list
./stack-explorer search "panic recovery"

# Serve the REST API, or the MCP endpoint
./stack-explorer serve --port 8081
./stack-explorer mcp --transport stdio
```

The database lives at `data/stack-explorer.db` when run from the repo root,
otherwise at `~/.stack-explorer/stack-explorer.db`; override it with `--db`.
`repos.example.yaml` is a starting catalog — copy it to `repos.yaml` (which is
gitignored) to keep your own, including `local_path` entries for repos you
have cloned locally.

Quantitative scans come from the Hadron blueprints in `blueprints/`; the
scripts in `scripts/` batch scans, ingest their output, and generate reports.

## Network and authentication

`stack-explorer serve` binds to `127.0.0.1` and accepts browser requests only
from the local Sigil frontend (`http://localhost:3334`; change it with
`--cors-origin`). On loopback no token is needed.

To reach the API from another machine, set a bearer token. The server refuses
to bind a non-loopback interface without one:

```bash
export STACK_EXPLORER_API_TOKEN='replace-with-a-long-random-token'
./stack-explorer serve --host 0.0.0.0 --port 8081

curl -H "Authorization: Bearer $STACK_EXPLORER_API_TOKEN" \
  http://your-host:8081/api/repos
```

The token is a single shared secret over plain HTTP. Put a TLS-terminating
proxy in front of it for anything beyond a trusted network. The MCP HTTP
transport (`stack-explorer mcp --transport http`) also defaults to
`127.0.0.1` and has no auth; keep it on loopback.

## Documentation

- [`docs/mcp-tools.md`](docs/mcp-tools.md) — MCP tool reference
- [`docs/audit-format.md`](docs/audit-format.md) — the audit folder format
- [`docs/product-vision.md`](docs/product-vision.md) — data model and direction

## Develop

```bash
make test
make lint           # go vet ./...
```

`make eval` rewrites the tracked `eval/baseline.json`. Run it only when you
intend to move the retrieval baseline, and commit that diff deliberately.
