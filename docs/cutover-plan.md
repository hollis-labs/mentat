# Cutover plan: mentat onto Substrate (DRAFT; option A ruled by the driver 2026-11-12, owner approval of the plan pending)

Mentat is in use: one running service (`mentat serve`, loopback port 8081) over a SQLite file of roughly 739 MB
(figure from `deploy.yaml`; re-measure on the rehearsal copy). All data is migrated with no loss. Nothing below
has been run against real data; the live database and service are untouched. Numbers marked *(copy)* come from a
`sqlite3 .backup` copy that the driver provides.

## 1. What is in the file

Schema source: `internal/store/sqlite/migrations/` (24 files, versions 001-027 with 017, 020, 025 absent),
applied by a hand-rolled runner that records them in `schema_migrations(version, applied_at)`.

| Group | Tables | Notes |
|---|---|---|
| Catalog | `repos`, `tags`, `repo_tags`, `snapshots`, `comparison_sets`, `comparison_set_repos` | `repos.embedding_profile` added by 024 |
| Scoring | `review_dimensions`, `lenses`, `lens_dimensions`, `scorecards`, `dimension_scores`, `architecture_patterns`, `repo_patterns` | `review_dimensions` and `lenses` are seeded by migrations 010, 012, 013 and may have been edited since |
| Findings | `findings`, `audits`, `audit_themes`, `finding_themes`, `report_configs`, `report_config_filters`, `scans` | |
| Code | `symbols`, `code_references`, `relationships` | largest tables expected *(copy)* |
| Embeddings | `embeddings` (vector BLOB, `dim`, `content_hash`, UNIQUE(target_kind,target_id,model)) | likely the bulk of the 739 MB *(copy)* |
| Jobs | `schedules`, `jobs`, `job_events` | operational; see section 5 |
| FTS5 (external content) | `findings_fts`, `symbols_fts`, `code_references_fts` | plus 9 triggers (`*_ai`, `*_ad`, `*_au`) |
| Bookkeeping | `schema_migrations` | replaced by goose's `goose_db_version` |

Row counts by table: *(copy)*. `mentat db stats` prints the same counts for 18 tables and is the before/after tool.

## 2. Two ways to carry the data (driver rules; do not choose yet)

### Option A: copy into a new file (guide position, recommended default)
1. Provide a copy `old.db` (`sqlite3 .backup`, driver action). Stop nothing.
2. New binary, new data dir, database module opens an empty file and runs one squashed goose migration
   (`00001_init.sql`) that creates the final schema: all tables, the 3 FTS5 tables and their triggers, the
   indexes. Identity-module tables arrive in the same file from the module.
3. A one-way command (`mentat migrate-legacy --from old.db`, run by the operator, refuses a non-empty target)
   copies each table in dependency order inside one transaction per table, with the original ids (many tables
   are referenced by integer id and by the Sigil client), then rebuilds the FTS5 indexes with
   `INSERT INTO x_fts(x_fts) VALUES('rebuild')` and resets `sqlite_sequence`.
4. Checks before the cut: per-table `COUNT(*)` equal old to new; `PRAGMA integrity_check`, `foreign_key_check`;
   `max(id)` and `sqlite_sequence` equal; for each FTS table `INSERT INTO x_fts(x_fts) VALUES('integrity-check')`
   and a sample of 20 MATCH queries returning equal rowids; embeddings: equal count and a byte-hash of
   `vector` per (kind,id,model) sample plus a retrieval eval run (`make eval` against `eval/baseline.json`) on old
   and new giving the same ranking.
5. Cost: reads and writes about 739 MB plus a rebuild of three FTS indexes and the new file's own size; duration
   *(copy)* (the `deploy.yaml` backup took 0.7 s, so the copy is fast; the FTS rebuild is the unknown).
   Needs about 1.5x the old size in free space for the new file, the WAL, and the copy.
6. Rollback: the old file is never opened for writing by the new binary. Stop the new service, start the previous
   release (kept by fleet-install) on the old file. Writes made after the cut are lost unless exported; state a
   freeze window for the cut.
7. Risks: the guard against copying a table whose columns the squashed schema renamed (the squash must match the
   final shape of every table including `ALTER`s from 021, 024, 027); ids must be preserved.

### Option B: adopt the existing file in place
1. Back up with `sqlite3 .backup` (as `deploy.yaml` already does) and keep it as the rollback.
2. A pre-start step in `main` (before `boot.Start`; the framework has no pre-migration hook, see Gap 4) checks
   `schema_migrations` holds exactly 001-027 as recorded, then creates `goose_db_version` marked at the baseline
   version and drops `schema_migrations`. The squashed `00001_init.sql` becomes the goose baseline and is only
   ever run on a fresh file.
3. Identity-module tables are added to the existing file at first start by the identity module.
4. Checks: the same per-table counts and integrity checks, taken before and after on a copy (rehearsal), and a
   check that the live schema text (`sqlite_master`) equals the one `00001_init.sql` creates on a fresh file;
   otherwise the two paths diverge.
5. Cost: seconds, no copy, FTS indexes and ids untouched.
6. Rollback: restore the pre-cut backup over the file and run the previous release. Everything written since the
   cut is lost, and the only live copy was mutated.
7. Risk: the schema equality check in step 4 can fail for reasons nobody sees today (24 hand-applied migrations
   over a long-lived file); a fresh-file schema and a long-lived one may differ in column order or defaults.

| | A copy | B in place |
|---|---|---|
| Live file touched | never | yes (rewritten metadata) |
| Rollback | relink, no restore | restore backup |
| Downtime | freeze window while copying (duration *(copy)*) | seconds |
| New code | migrate-legacy command and its tests | baseline step and schema-equality test |
| FTS / embeddings | rebuilt / copied byte for byte | untouched |

## 3. Rehearsal and approval
1. Driver supplies a copy; I never open the live file.
2. Rehearse the chosen path on the copy; record counts, checks and time here.
3. Owner approves this plan; only then does any code touching the store land.
4. Deploy: build in CI, `fleet-install deploy mentat --commit <sha> --dry-run`, then for real; previous release kept.
5. Live verification: sign-in via setup, one write, one read through the real URL, an unknown `Host` refused,
   `/healthz` 200, an MCP call.

## 4. Clients (every caller of the old surface)

| Client | Old contact | What changes | Owner |
|---|---|---|---|
| Sigil frontend (named in code and README; CORS default `http://localhost:3334`) | REST `/api/*`, optional bearer token, `{error}` bodies, no CSRF header | sign in through identity (cookie or API key), send `X-Requested-With` on writes, read `{code,message}` errors, same-origin or `identity.allowed_origins` instead of CORS | Sigil owner (task CW-20261112-0086 is sigil; confirm it covers this) |
| fleet-install / `deploy.yaml` health check | `GET http://127.0.0.1:8081/api/repos`, expects 401 | `/healthz` expects 200 (the file says to switch once it exists); `sqlite_backup` preflight path and `restart: systemd-user mentat` change with the data dir | driver / Cerberus owner |
| Cerberus descriptor `stack-explorer.cerberus.yaml` | `./mentat serve --port 8081`, `dir: .../apps/stack-explorer`, launchd | command without `--port` (port from `httphost.addr`), new dir; the file also still names the stack-explorer id | Cerberus owner |
| systemd user unit `mentat` (hand-written, on the agent host) | `mentat serve --db ~/.local/share/mentat/stack-explorer.db`, port 8081 | the `--db` flag is gone; data dir and `SUBSTRATE_CONFIG` replace it | driver / host owner |
| MCP clients (agents) | `mentat mcp --transport stdio`, or `--transport http` on port 8090, no auth | HTTP MCP is served by the main listener at `/mcp` with scope `mentat.read` (tools check their own scope); stdio keeps working with `stdio_token_file`; clients need a token | agents' config owners |
| Hadron blueprint `blueprints/se-repo-scan.yaml` and `scripts/*.sh` | the CLI (`mentat ...`) | CLI keeps its commands; list in Gap 6 whether any pass `--db` | mentat |
| Operators / shells | `STACK_EXPLORER_*`, `SE_EMBED_*` env, `--db`, `--token` | see `conversion-notes.md` | operator |

## 5. Rehearsal 1 result (copy of the rehearsal master, 2026-11-12)

- `migrate-legacy` took 22.6 s (run 1) and 14.1 s (run 2, with the schedule step) on a 738,979,840-byte source; run 2 left 362,856,448 bytes and a 0-byte WAL. All 27 tables: equal row counts and content digests;
  FTS hits identical by rowid; `integrity_check` ok; autoincrement counters >= source. The source hash was unchanged and
  a second run is refused. Disk: expect about 1.5x the source transiently; the command now checkpoints and truncates the
  WAL before it reports success and prints the final file sizes (the first rehearsal left a WAL as large as the data).
- Rollback: the old file is only attached read-only; keep it and the previous release, relink the previous release.
- **For the owner:** the source has one orphan `job_events` row (rowid 14, its job does not exist) and the migrated
  database has the same one. It is carried unchanged (no loss); whether to delete it afterwards is the owner's call.
  Foreign keys: the migration copy runs with them OFF so the orphan can be carried; the ported app writes through one
  connection and it enforces them (`TestForeignKeysAreEnforcedOnWrites`; the read pool never writes), and the old `schedules` table (and its foreign keys) is gone, so the
  history columns `jobs.schedule_id` and `job_events.schedule_id` are plain labels. The orphan does not make a normal
  operation fail: on the migrated copy, start, the `/api/events` backlog read, a job run through the queue and a
  scheduled fire (at the next 5-minute boundary) all worked (rehearsal 2).
- Owner approval of this plan is still required before anything is deployed.
