# Changelog

All notable changes to Stack Explorer are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Stack Explorer is pre-release and has no tagged versions: there is no compatibility promise, and breaking changes can land in any commit. Backfilled from `git log`; good-faith, not exhaustive.

## [Unreleased]

### Security

- Go toolchain 1.26.10 (`go` directive and CI pin) for GO-2026-6809 (crypto/x509); no behavior change.

### Added

- Open-source project documents: `CHANGELOG.md`, `CONTRIBUTING.md`,
  `SECURITY.md`, `TRADEMARK.md`; `AGENTS.md` is now tracked.
- Bearer-token authentication for the REST API (`--token` /
  `STACK_EXPLORER_API_TOKEN`), and a `--cors-origin` flag.

### Changed

- Adopt the released monorepo packages: embedding contracts from
  `substrate/llm-core` v0.1.0 and MCP server/transports from `libs/plugin-mcp`
  v0.1.1. Remove the corresponding standalone module dependencies.
- Require Go 1.26.8 and select the patched Go 1.26.9 toolchain.
- Check the pure-Go build, vet and race suite in GitHub Actions.

- **`stack-explorer serve` now binds `127.0.0.1` by default** and refuses a
  non-loopback bind without a token; it previously listened on every interface
  with no authentication and CORS open to any origin.
- Module path renamed to `github.com/hollis-labs/mentat`.
- The MCP server adopted `go-mcp`, dropping the direct
  `modelcontextprotocol/go-sdk` dependency.
- README rewritten as a pre-release identity and stack-fit document.

## Pre-release history

### May 2026

- Repo tag sync and GitHub authentication for scans.
- Finding status validated against a canonical allowed set, and a
  `finding update-status` subcommand.
- **Breaking:** dropped `go-providers` in favor of `go-embed-contracts` for
  embeddings.

### April 2026

- Jobs API route, job recovery on startup, model provenance on import, and
  stable content hashes on re-ingest.
- Graph and co-change foundations, a relationships migration and writers,
  graph query surfaces, and hardened SCIP relationship extraction.
- Phase A–E build-out: audit ingestion and surfaces, symbol indexing and query
  surfaces, hybrid retrieval (FTS5 BM25 plus vector search) with embedding
  refresh, an MCP surface, and a scheduler runtime.
- Initial commit: repo catalog, lenses and scoring.
