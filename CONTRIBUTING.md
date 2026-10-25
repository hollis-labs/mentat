# Contributing

How a change gets from your clone into `main`. This is deliberately short: most
of what you need is written somewhere closer to the thing it describes.

## Before your first change

`README.md` has the install and quick-start steps. `AGENTS.md` is the fastest
orientation to the layout and the boundaries that are not obvious from reading
the code. Seed a scratch catalog from `repos.example.yaml` and point `--db` at
a throwaway file while you work, so your experiments never touch a catalog you
care about.

## The sequence

1. **Branch.** `<type>/<short-slug>`, where the type matches the change —
   `feat`, `fix`, `docs`, `chore`. Nothing enforces this; it is what the
   history does.
2. **Change one thing.** A branch carrying two unrelated changes costs the
   reviewer the ability to accept one and question the other.
3. **Run the checks** before you push:
   ```
   make lint && make test
   ```
   If you touched `internal/symbols/`, also run
   `go test -tags treesitter ./internal/symbols/...` (needs CGo).
4. **Push and open a pull request.** A maintainer will review it.

Commit subjects follow the conventional-commit shape — a type, an optional
scope, a colon, then the summary.

## What a pull request should carry

The reviewer was not there when you made the decisions. State what the change
does, what it deliberately leaves alone, and the evidence that it works — the
commands you ran and what came back, not a claim that it passes. If you changed
retrieval, say whether `make eval` moved and by how much.

Add a line to `CHANGELOG.md` under `[Unreleased]`.

## The one that cannot be undone

**Schema migrations.** The catalog is a SQLite database users keep and migrate
in place. Migrations under `internal/store/sqlite/migrations/` are numbered,
embedded, and applied on open; never edit or renumber one that has shipped, and
never reuse a number. Add a new migration, and test it against a copy of an
older database, not only a fresh one.

## Things that surprise people

- **`make eval` rewrites `eval/baseline.json`.** Run it only when you intend to
  move the retrieval baseline, and commit that diff deliberately.
- **Tree-sitter is behind a build tag.** `make test` neither compiles nor
  exercises it.
- **Sort parameters come from query strings.** Every list handler must route
  them through `allowedSort()` in `internal/api/server.go`.
- **The default listener is loopback.** Keep it that way; see `SECURITY.md`.
- **Stack Explorer does not scan code.** Quantitative scans come from the
  Hadron blueprints in `blueprints/`.
- **Historical docs cite machine-specific paths.** A path in a document is not
  evidence the path exists.

## What this does not cover

- **Which change is worth making.** Open an issue to discuss larger features
  before building them.
- **Releases.** Releases are tagged `0.x` and published from CI-built artifacts; a maintainer cuts them.
