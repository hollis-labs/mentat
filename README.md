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
- **A REST API.** `mentat serve` runs the HTTP API over the same
  catalog, bound to `127.0.0.1` with signed-in access (cookie or API key); `internal/jobs/` runs scheduled refresh work
  (`mentat schedule`).

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
`mentat report generate` and `mentat scorecard generate`.

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
go install github.com/hollis-labs/mentat/cmd/mentat@latest
```

or build from source:

```bash
git clone https://github.com/hollis-labs/mentat.git
cd mentat
make build          # -> ./mentat, pure Go
make install        # or install to $GOBIN
```

`make build-full` adds the `treesitter` build tag for tree-sitter symbol
extraction — the only CGo path in the repo.

## Quick start

```bash
# Seed a catalog from the example manifest (public repos only)
./mentat repo import repos.example.yaml
./mentat repo list | head

# Lenses, scores, search
./mentat lens list
./mentat search "panic recovery"

# Serve the REST API, or the MCP endpoint
./mentat serve
./mentat mcp
```

Mentat keeps its data (database, identity key, secrets) in one private data
directory: `MENTAT_DATA_DIR` if set, otherwise the user data directory for
"mentat". Settings come from the manifest and `SUBSTRATE_CONFIG`; there are no
`--db`, `--port` or token flags and no `STACK_EXPLORER_*` environment variables.
To bring a pre-Substrate database across, see `docs/cutover-plan.md` and
`mentat migrate-legacy --from <old db>`.
`repos.example.yaml` is a starting catalog — copy it to `repos.yaml` (which is
gitignored) to keep your own, including `local_path` entries for repos you
have cloned locally.

Quantitative scans come from the Hadron blueprints in `blueprints/`; the
scripts in `scripts/` batch scans, ingest their output, and generate reports.

## Network and authentication

`mentat serve` binds to `127.0.0.1` (`httphost.addr`) and refuses a
non-loopback address and any request whose `Host` is not allowed
(`mentat.allowed_hosts`). Open `GET /api/setup` once to create the owner, then
sign in; scripts and agents use identity API keys. Scopes: `mentat.read`
(every GET), `mentat.write` (POST/PUT/DELETE), `mentat.admin` (database backup,
audit import/export). Browser writes send `X-Requested-With`. The MCP endpoint
is served by the same listener at `/mcp`; `mentat mcp` serves it over stdio and
needs the token file the framework requires, holding an identity API key. Put a TLS-terminating proxy in front
for anything beyond a trusted network.

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
