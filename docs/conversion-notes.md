# Conversion notes: mentat onto Substrate (WORKING DRAFT)

Status 2026-11-12: analysis only, no code changed. Method: `docs/adopting-substrate.md`. Cutover: `docs/cutover-plan.md`.

## Starting point
- `go.mod` imports only `github.com/hollis-labs/substrate/llm-core v0.1.0` (embedcontracts). No framework module,
  manifest or wiring exists. The earlier rebrand branch (`rebrand/mentat-binary`) only renamed the module and binary.
- Mentat is in use; the brief's "fresh" was wrong (driver ruling: migrate all data).

## Gaps

1. **Data path undecided (A copy vs B in place).** See `cutover-plan.md` section 2. Needs the driver's ruling.
2. **Auth change breaks Sigil.** The bearer token (`STACK_EXPLORER_API_TOKEN`) is replaced by identity + `enforce`.
   Needs the owner to say which scopes (`mentat.read` / `mentat.write` / `mentat.mcp`) and whether a long-lived
   API key for Sigil and agents is acceptable. Not decided here.
3. **Existing routes outside Substrate's contract.** `GET /api/events` is a long-lived SSE stream and
   `GET /api/db/backup` streams the whole database file. Both need checking against `httphost` (timeouts, write
   deadlines) and, for the backup route, a decision: keep, scope `mentat.admin`, or drop in favour of
   `sqlite3 .backup`. Not verified yet.
4. **No pre-migration hook** (known). Option B and any legacy-layout guard must run in `main` before `boot.Start`.
5. **MCP library version.** Mentat is on `github.com/hollis-labs/libs/plugin-mcp v0.1.1` (old module path); the BOM
   pins `github.com/hollis-labs/libs/mcp/plugin-mcp v0.6.0` and the `mcp` module mounts `/mcp` itself. Moving the
   tool set to the module is a port (tools stay), but the old HTTP transport on a second port (8090) goes away.
6. **CLI model.** The CLI (17 command groups) opens the database with a global `store` and a `--db` flag and
   runs while the service runs (WAL). With `cli.Root(run.App)` and `boot.Start` every command would start the
   modules. Need to confirm that `boot.Start` for a CLI command does not bind the HTTP port and opens the same
   database file safely beside a running `serve`. Not verified.
7. **Pins.** `chi`, `cors`, `uuid`, `openai-go`, `robfig/cron`, `scip`, `tree-sitter*`, `protobuf`, `modernc sqlite`
   are not in the BOM `third_party`. `pins check --app .` has not been run; if it fails these are BOM PRs to Substrate,
   not local exceptions. chi/cors/jobs are expected to go (httphost, scheduler/queue modules) but the scan
   code still needs tree-sitter (CGo, behind the `treesitter` build tag), which needs a ruling because
   golangci-lint and CI build without it.
8. **Jobs and schedules.** `internal/jobs` (robfig cron, a worker pool, `schedules`/`jobs`/`job_events` tables, SSE)
   overlaps the `scheduler` and `queue` modules. Port or replace is a decision; a replacement must keep the three
   tables' data (migrated) and `MarkInProgressJobsFailed` startup recovery semantics.
9. **Manifest cannot carry the app's own config** (known framework gap); `mentat:` block read from
   `app.Config["mentat"]`, defaults in `internal/settings`.
10. **Stale naming.** `AGENTS.md`, `README.md`, the Cerberus descriptor and the DB file name still say
    stack-explorer. Renaming the DB file is a data-path question (part of Gap 1); docs I can fix freely.

## Environment (old to new, proposed)

| Old | New |
|---|---|
| `--db`, default `data/stack-explorer.db` or `~/.stack-explorer/stack-explorer.db` | `database.sqlite.path`, data dir resolved in `main` (XDG data home, `MENTAT_DATA_DIR` override). Note the live file is under `~/.local/share/mentat/` |
| `serve --host/--port`, default 8080 | `httphost.addr` (loopback required) |
| `--token`, `STACK_EXPLORER_API_TOKEN` | removed; identity (Gap 2) |
| `--cors-origin` | removed; same origin or `identity.allowed_origins` |
| `mcp --transport http --host/--port/--path` | `/mcp` on the main listener; `stdio` kept |
| `SE_EMBED_{SMALL,MEDIUM,FULL}_{PROVIDER,MODEL}`, `SE_EMBED_OPENAI_BASE_URL` | `mentat.embed.*` config keys |
| `SE_EMBED_OPENAI_API_KEY`, `OPENAI_API_KEY`, `GITHUB_TOKEN` | secrets module references |
| `STACK_EXPLORER_{MCP_ACTOR_KIND,MCP_ACTOR_ID,SESSION_ID,MCP_MODEL,ACTOR_ID,MODEL}` | taken from the verified principal where possible; otherwise `mentat.actor.*` config. Decision needed |

## Rulings and findings, 2026-11-12 (driver)

- **Gap 1 resolved: option A** (one-way migration into a new file; the old file is never written). Rehearsal master is
  a read-only copy held by the driver on the build host; it holds real data and never enters the repo, a fixture, a
  log, a mail or a PR. Tests use synthetic fixtures. Copy row counts (driver, 2026-11-12): relationships 703,936;
  job_events 127,679; jobs 31,928; symbols 22,184; embeddings 13,776; code_references 1,268; tags 389;
  repo_tags 706; dimension_scores 659; findings 360; snapshots 200; repos 169; scans 126; scorecards 48;
  audits 39; architecture_patterns 26; repo_patterns 25; lenses 9; comparison_sets 7; report_configs 5; schedules 1.
- **Gap 2 resolved:** scopes `mentat.read`, `mentat.write`, `mentat.admin`; non-human clients use identity API keys.
- **Gap 3 resolved:** keep both. `GET /api/events` is `mentat.read`. `GET /api/db/backup` is `mentat.admin`. Note for the
  owner: it streams the whole database file; whether it stays is the owner's call.
- **Gap 6 verified (prototype, scratch data dir, since removed).** A minimal `boot.Start` + `cli.Root` binary with
  the manifest's modules: `serve` bound `httphost.addr`; while it ran, `dbping`, `health` and `version` as
  separate processes on the same SQLite file and the same identity key file all succeeded; the CLI did not bind a port;
  the key file hash was unchanged before and after; `/healthz` stayed `{"status":"ok","checks":{"database":"ok"}}`;
  an unauthenticated `GET` on a scoped route answered 401 and `GET /api/setup` answered `{"setup_required":true}`.
  Not verified: two writers at once under load; SIGTERM exit code (the process exited, the code was not captured).
  Consequence: no framework Gap here. The queue and scheduler modules offer `autostart: false` for exactly this
  case; whichever is used must be started from `serve` only.
- **Gap 7 partly answered.** After moving plugin-mcp to `github.com/hollis-labs/libs/mcp/plugin-mcp v0.6.0` and
  adding the framework, `go build ./...`, `go vet ./...` and `substrate pins check --app .` pass. Exact outputs:
  before the move, `go.mod: unpinned: github.com/hollis-labs/libs/plugin-mcp is a libs module the bom does not pin (add it to
  app_libs or drop the import)`; after, `app pins match the bom`. robfig/cron, openai-go, scip, tree-sitter,
  protobuf, chi and cors are not checked by `pins check` (it only checks libs and the framework), so no BOM change is
  needed for them. The framework raised `modernc.org/sqlite` from v1.48.1 to v1.60.1 (the BOM's), which the
  jobs/FTS tests must now run on. The `treesitter` build tag is kept as is (CGo, `make build-full`).
  chi and cors are dropped as ruled during the route port.

## Gap 8: jobs, job_events, schedules versus the queue and scheduler modules (mapping, no decision)

What mentat holds today (migration 026, volumes from the copy):

| Table | Holds | Rows |
|---|---|---|
| `schedules` | id (text), repo_id FK, name, cron_expr, job_kind, payload_json, enabled, max_attempts, retry_backoff (`fixed`/`linear`/`exponential`), retry_delay_secs, last_run_at, next_run_at | 1 |
| `jobs` | id (text), schedule_id FK, repo_id FK, kind, status (`queued`, `in_progress`, `completed`, `failed`, `skipped`), attempt_count, max_attempts, retry settings, payload_json, output_json, error, started/finished/created/updated_at | 31,928 |
| `job_events` | autoincrement id, job_id FK, schedule_id, repo_id, job_kind, event_type (`queued`, `started`, `progress`, `attempt_failed`, `retry_scheduled`, `completed`, `failed`, `schedule_error`), status, message, payload_json, created_at | 127,679 |

Behaviour the app relies on: six job kinds (`scan`, `symbol-reindex`, `embedding-refresh`, `audit-refresh`,
`git-history-sweep`, `complexity-metrics`); a retry loop inside the worker; per-job `output_json` read back by the
API and MCP; `WaitForTerminal`; an SSE stream and `GET /api/jobs` that filter by repo, schedule, job, kind, status
and `since_id`; progress events during a run; startup recovery (`MarkInProgressJobsFailed`); and a manual trigger of a
schedule or kind.

What the modules provide (read from Substrate and libs core/util v0.6.0, not run):

| Need | queue module (libs `queue`, sqlite driver) | scheduler module (libs `scheduler`, `sqlstore`) |
|---|---|---|
| Durable queue, workers, retries | yes: tables `queue` rows with `type`, `payload` BLOB, `attempts`, `max_tries`, `reserved_at`, `available_at`, plus a failed table | |
| Cron schedules | | yes: `Schedule{ID, CronExpr, JobType, Payload, Retry, Location, Overlap, Misfire, ...}`, own tables |
| Job history (status, output, error, timings, 31,928 rows) | **no**: a queue row exists only until done; the failed table keeps `error`, `attempts`, `failed_at` for failures only; no `output`, no completed history | fires are retained by `KeepLastN`/retention, not as an app history |
| Event stream (127,679 rows, filters, `since_id`, SSE) | **no** | observer/metrics hooks exist, no persisted event log |
| Status vocabulary, `skipped` | no | no |
| Payload as JSON text | BLOB, can carry it | BLOB |
| Retry backoff fixed/linear/exponential | `max_tries` only | `BackoffNone/Constant/Linear/...`, `exponential` to be confirmed |
| Repo FK and ids | no | no (string ids) |

Conclusion for the decision: the modules can carry execution (workers, retries, cron firing), but not the history.
`jobs` (with outputs), `job_events` and `schedules` (with names, `last_run_at`, `next_run_at`, repo FK) would stay
as the app's own tables and be migrated as they are. Options for the driver to rule on, none started:

1. Keep `internal/jobs` as the app's engine over the app's own three tables (cron via robfig/cron, worker pool);
   register nothing with the queue and scheduler modules. Smallest change, keeps behaviour, duplicates mechanism.
2. Use the queue module to execute and keep `jobs` / `job_events` as app-owned history written by the handlers;
   use the scheduler module for the cron schedules and give it a custom `Store` over the app's `schedules` table
   (`scheduler.UseStoreFunc`; libs ships a conformance suite for it). Removes robfig/cron and the worker pool.
   Needs: the app's history writes around each handler, a mapping of retry settings to the queue's `max_tries`
   (the queue has no per-job backoff in what I read), and a Gap if backoff per job is required.
3. As 2, but adopt the schedulers' own tables and drop mentat's `schedules` (1 row): loses name, repo FK and
   `last_run_at` semantics unless carried in payload. Not recommended.

Open question for the driver: is per-job backoff (`linear`, `exponential`) and a persisted event stream required to
survive (the SSE API says yes), which rules out a pure module replacement.

## Decisions 2026-11-12 (second ruling)

- **Gap 8 ruled: option 2.** The queue module executes, the scheduler module fires over a Store on mentat's own
  `schedules` table (`scheduler.UseStoreFunc`; the libs conformance suite for a custom store must run in CI), and
  `jobs` and `job_events` stay mentat tables (history and the log behind `/api/events`), written by the handlers and
  migrated as they are. robfig/cron and the worker pool go. The jobs engine is ported last. Both modules are set
  `autostart: false` and started from the `serve` command only (`queue.Run(app)`, `scheduler.Run(app)`), so a CLI command
  never runs jobs or fires schedules.
- **Backoff, from the source.** `libs core/util/queue` has per-job `WithMaxTries` and `WithDelay`, but the retry delay is
  one worker-wide `RetryAfter` (`Release(ctx, id, opts.RetryAfter)`); there is no per-job or growing backoff.
  Mentat's `retry_backoff` (`fixed|linear|exponential`) and `retry_delay_secs` are per job. In the rehearsal copy (counts
  only): all 31,928 jobs are `git-history-sweep`, `fixed`, `max_attempts` 1, delay 1; none was ever retried
  (`attempt_count > 1`: 0); the only schedule is `git-history-sweep`, cron `0 */5 * * * *`, fixed, 1 attempt. Nothing in
  production data uses linear or exponential. Open for the driver: drop the two growing policies (and the CLI flag values)
  as unused, keeping `max_attempts` and a fixed delay, or file a libs Gap for per-job delay.
- Observation, not acted on: of those jobs 28,961 are `failed`, 2,966 `completed`, 1 `queued`. The sweep fails most
  runs on the live service today; the migration carries the history as is.
- **modernc.org/sqlite 1.48.1 -> 1.60.1** came with the framework (the BOM's). The old file will be read by 1.60.1, so the
  rehearsal must prove FTS5 and BLOB behaviour under it.
- **Evidence owed, now shown** (rehearsal on a copy, explicit temp dirs): five CLI writes while `serve` was running all
  succeeded and were visible through the API; SIGTERM gave exit code 0.

## HTTP contract changes (backend, for the Sigil client)

- Paths are exact `net/http` patterns: `/api/repos/` with a trailing slash no longer matches (chi accepted both).
  Collection and item paths are otherwise unchanged (see `internal/api/server.go`, `routes()`).
- Errors are `{code, message, details?}` instead of `{error}`; `code` is one of `bad_request`, `unauthorized`,
  `forbidden`, `not_found`, `conflict`, `invalid`, `internal`, `error`. Success bodies (`{data, meta}`) are unchanged.
- Auth: the shared bearer token is gone. Sign in through the identity routes (cookie) or present an identity API key;
  browser writes need `X-Requested-With` and a same-origin `Origin`. CORS is removed.
- Scopes: `mentat.read` for every GET, `mentat.write` for POST/PUT/DELETE, `mentat.admin` for `GET /api/db/backup`.
  MCP tools: `audit_import` and `audit_export` take a path on the server's filesystem (a file read and write
  primitive), so they need `mentat.admin` (a tightening with client impact: an agent key that imported audits needs it);
  `finding_add` and `finding_update_status` need `mentat.write`; the other tools `mentat.read`. Behaviour is otherwise unchanged.

## Gap 11: one write connection, a read pool, no nested cursors (approved 2026-11-12)

The framework store has one write connection and a separate read-only pool; a second statement while a cursor is open
blocks. The old code nested queries and used one pool. Fix: `internal/store/db` is a facade (reads through the pool,
writes through the writer or `WriteTx`); the nested cursors in `audits.ListAuditFindings`, `mcp.scanFindingSummaries`
and `api.listReports` now close before the next query; `sqlite.Open` uses ONE read connection so a regression hangs a
test. Tests: `TestLongReadDoesNotBlockAWrite`, `TestEveryReadRouteFinishesWithOneReadConnection`.

## Behaviour changes (all)

- Auth: bearer token, `STACK_EXPLORER_*` and `SE_EMBED_*` env, `--db`, `--token`, `--cors-option` are gone; identity
  sessions or API keys, the manifest `mentat:` block, `SUBSTRATE_CONFIG` and `MENTAT_DATA_DIR` replace them.
- Cookie writes need `X-Requested-With` (seen in the rehearsal: 403 `csrf` without it). CORS and chi are removed.
- Errors are flat `{code, message}`; trailing-slash paths no longer match; `/healthz` replaces the 401 health probe.
- `GET /api/events` needs `mentat.read`; `/api/db/backup` needs `mentat.admin` and streams the whole database (owner
  note); `audit_import`/`audit_export` need `mentat.admin`.
- MCP: the HTTP MCP port 8090 and the `X-Stack-Explorer-*` provenance headers are gone; provenance comes from the
  verified principal. `mentat mcp` (stdio) no longer runs the job engine; only `serve` does.
- The server refuses a non-loopback `httphost.addr` and an unknown `Host` (403).
- Startup: `serve` marks jobs left in progress as failed; opening the store no longer does.
- Dropped feature (owner, 2026-11-12): the linear and exponential retry policies. `max_attempts` and one fixed retry
  delay stay. Evidence: of 31,928 jobs none was retried (`attempt_count > 1`: 0) and all used `fixed`. The columns
  `retry_backoff`, `max_attempts`, `retry_delay_secs` are migrated unchanged; history is not rewritten.
- modernc sqlite 1.48.1 -> 1.60.1 with the framework; FTS5 and BLOB behaviour proven by the rehearsal.

## Known issues (pre-existing, not part of the port)

- `severity_rank()` is called in the `ListAuditFindings` ORDER BY but looks unregistered, so the order falls back to
  the next sort key. Not fixed here (driver ruling). Record the query and observed order when the jobs work lands.
- The git-history sweep fails most runs in production (failed 28,961, completed 2,966, queued 1; last completed
  2026-05-14: `git log failed: exit status 128`, missing chdir path). With the owner. The jobs port adds a sweep test.
- One orphan `job_events` row (rowid 14 -> jobs) exists in the source and is carried unchanged.

## stdio MCP launch

`mentat mcp` is launched by the MCP client's own config (an agent on this host starts it as a child process), and the
framework requires a token file for stdio today (`mcp.stdio_token_file`). Local filesystem trust and an offline
bootstrap command are future framework changes; no workaround is built here. Who launches it: agents' config owners
(see the client list in `cutover-plan.md`).

## History findings (for the planned rewrite pass; history is not touched by the port)

`python3 scripts/scan-public` on 2026-11-12, 56 commits, 21 findings; the tree is clean (`--tree`). Values are not
repeated here.

| Rule | Files (first commit that added them) | Count |
|---|---|---|
| host `agent-box-host` | a commit message (a347a090ec35), `.agent-ops/project.yaml` and `AGENTS.md` (53cbdab3f863) | 3 |
| private-path `home-path-macos` | `AGENTS.md` (53cbdab3f863); `CLAUDE.md`, `blueprints/se-repo-scan.yaml`, `docs/install-on-new-machine.md`, `docs/stack-explorer-api-prompt.md`, `repos.yaml`, 9 files under `skills/` (f3e1eb5c6001); `docs/superpowers/plans/2026-04-16-longmemeval-vanta.md` (b0dc192a4081); `stack-explorer.cerberus.yaml` (f88c4ef486c6) | 16 |
| secret `secret-assignment` | `README.md` (faf0fd5ba50a): the placeholder value of the old API token variable, not a credential | 1 |
| allowlisted (tree and history) | `deploy.yaml` fleet inventory alias (ae873493ef31) | 2 |

## Lint

`golangci-lint` v2.11.4 (run with `--max-issues-per-linter=0 --max-same-issues=0`, so the display cap hides nothing)
found 153 issues in the repo (the default display showed 51): 137 errcheck, 11 staticcheck, 5 unused. All are fixed
in their own commit with no behaviour change: ignored errors are now explicitly discarded (`_ =`), `WriteString(Sprintf)`
became `Fprintf`, dead code was removed. Two kept with a reasoned `//nolint:staticcheck`: `strings.Title` in the audit
export (the replacement adds a dependency and the output must stay identical) and the legacy SCIP descriptor suffix in a
test fixture. CI runs the same command as a gate.

## Jobs port (Gap 8, option 2, with the driver's ruling B for schedules)

**What runs where.** `mentat serve` starts the queue worker (`queue.Run`) and the scheduler engine (`scheduler.Run`);
both modules are `autostart: false`, so no other command (CLI, `mentat mcp`) runs a job or fires a schedule.
`TriggerJob` writes the `jobs` row and a `queued` event, then pushes an envelope onto the queue; the worker's handler
(`jobs.Service.Handle`) runs one attempt, writes `started`/`completed`/`attempt_failed`/`retry_scheduled`/`failed` events and
the job row. A scheduled fire arrives with no job row, so the handler creates it. `robfig/cron` and the in-process worker
pool are gone from the app (the cron library remains inside libs).

**Retry.** `max_attempts` and one fixed delay (`retry_delay_secs`) stay; a failed attempt with attempts left is pushed
again with `WithDelay`. The queue worker's `RetryAfter` is not used: the queue module does not expose it (it
is one worker-wide value, and the module config has no key for it), and the app's delay is per job. This is a
difference from the ruling's wording ("through the worker's RetryAfter"), recorded for the driver.
`retry_backoff` values stay in the rows and are not acted on (linear and exponential dropped by the owner).

**Framework limits met while porting (no workaround for a missing piece; the supported hooks were used):**
1. The queue module's SQLite driver hardcodes the table name `jobs`, which is Mentat's job history (31,928 rows).
   The module also gives a popped job 60 seconds before it is handed out again; a scan or sweep can run longer.
   Mentat supplies the driver itself through `queue.UseDriver` (libs `qsqlite.New` with tables `queue_jobs` and
   `queue_failed_jobs`, 6 hours reservation). `UseDriver` must be called before the database exists, so the driver is
   built on first use (`cmd/mentat/queue.go`). A `queue.table` / `queue.reserve_seconds` config key would remove that file.
2. The scheduler record has no name/repo field; see below.

**Schedules (ruling B).** The one production schedule moves into the scheduler module's reference store
(`gosched_*` tables, libs sqlstore); the old `schedules` table is gone from the new schema.
- The mapping is `internal/schedmap`. Every old field is kept: id, name, repo_id, job_kind, the cron expression as written,
  the job payload, enabled, `max_attempts`, `retry_backoff`, `retry_delay_secs`, `created_at`, `updated_at`, `last_run_at`
  (libs `LastRun`) and `next_run_at` (kept in the payload as `legacy_next_run_at`). The migration reads each record
  back through the store and compares all of these before the main copy; `schedule list --format json` returns the
  same shape as before. `next_run_at` shown is the recomputed one: the old engine overwrote it at every start
  (`registerSchedule`), so the stored value was never authoritative.
- `jobs.schedule_id` and `job_events.schedule_id` are now plain columns (no foreign key): deleting a schedule used to
  null them, now the history keeps the id. The migration proves the same rows carry a value, the same set of distinct ids
  (rehearsal: 31,928 rows, 1 distinct id, resolving to the migrated schedule), and `schedule list` shows that id.
- A repo's schedules were removed by `ON DELETE CASCADE`; now `DeleteRepo` (API and CLI) removes them first.
- Cron: the scheduler parses 5 fields. A 6-field expression whose seconds field is `0` is the same schedule and is
  accepted; any other seconds value is refused (migration: the move stops before writing, naming the schedule).
  `TestNextFireTimesMatchTheOldEngine` proves the next five fire times equal the old engine's (robfig with seconds) for five
  expressions at three fixed clocks. Zone: the old engine read cron fields in server local time; the scheduler needs a
  named zone: `mentat.schedule_timezone` (default UTC; the host's zone is UTC, so no change here).
- Misfire after downtime: the old engine, being in memory, fired nothing for ticks missed while it was down. The
  schedule is stored with `MisfireSkip`, which does the same (libs' default would run one catch-up fire);
  `TestTicksMissedWhileDownAreNotFired` proves a stale schedule skips to its next occurrence and an on-time tick fires once.
- Fire identity (`gosched_fires`) now makes a fire exactly-once across a restart; the old cron entry could fire twice or
  not at all around one.

**Events.** `Subscribe` polls `job_events` (250 ms), so the SSE stream and the MCP `events_subscribe` tool see
events written by any process; before, they only saw events of their own process.

**CLI.** `schedule trigger` queues the job and waits for it; with no `serve` running it times out and says jobs run
inside `serve`. `schedule add --cron` takes 5 fields, or 6 with seconds 0.

**Proof on the rehearsal copy (run 2).** `migrate-legacy` 14.1 s, 26 tables equal by count and digest, the schedule moved,
final file 362,856,448 bytes, WAL 0 bytes. Then `serve` on the copy: owner setup, `/api/events` backlog read (orphan present),
`schedule trigger` ran a job through the queue worker (it failed with a clear `git log failed: chdir ... no such file`
for a repo path from another machine: the production sweep failure, reproduced, with the worker alive), the scheduler
fired the schedule by itself at the next 5-minute boundary (job created, fire `succeeded`, queue empty), SIGTERM
stopped the server, `integrity_check` ok.
