# Cutover plan: Mentat onto Substrate

Status: DRAFT for the owner's approval. Nothing here has been run against the live service or its database; every
number comes from two rehearsals on a copy.

## 1. What this is and what the owner decides

Mentat runs today as one service (`mentat serve`, loopback port 8081) over one SQLite file of about 739 MB, and is in use.
The port replaces the binary and moves the data into a new file with a new layout (the framework's identity, queue
and scheduler tables sit beside Mentat's own). **No data is lost**: every row is copied, proved equal, and the old
file stays untouched as the rollback.

The owner is asked to decide:
1. **Approve this plan** and a time for the cutover.
2. **The failing git-history sweep.** In production 28,961 of 31,928 sweep jobs failed (last success 2026-05-14): the
   sweep runs `git log` in each repo's recorded path and that path does not exist on the host. The one schedule
   (`git-history-sweep`, every 5 minutes) can be carried over **enabled** (it keeps failing, now with a clear error
   and without harm) or **disabled** (set `enabled` off after the cutover with `mentat schedule remove <id>` and re-add
   when the paths are fixed). Default proposed: carried over enabled, unchanged behaviour.
3. **The orphan row.** The old database holds one `job_events` row (rowid 14) whose job no longer exists. It is
   carried over unchanged, because a migration drops nothing. Whether to delete it afterwards is the owner's call;
   nothing depends on it.
4. **Who changes which client** (section 6) and when.

## 2. What stops, and for how long

Only the service. During the cutover Mentat's REST API, MCP endpoint, background scan worker and schedule are down.
Expected window: about **2 minutes** (stop the service, run the copy, start the new one), plus the time to sign in
and verify. Rehearsal timings: the copy takes **14 to 23 seconds** for the 739 MB source. A freeze matters: anything
written to the old file after the copy starts is not in the new one, so the service is stopped before the copy.

## 3. Runbook

Prerequisites: the new release built by CI and installed beside the previous one (the fleet tool keeps the previous
release); free disk of at least **1.5 times** the old file (the new file ends at about 363 MB after the copy; the
copy itself needs more transiently); a backup of the old file (`deploy.yaml` already takes one before deploy).

1. Stop the service.
2. Copy the old file to a scratch name (`sqlite3 <old> ".backup <scratch>"`). The copy command never opens the old
   file for writing, but the scratch copy keeps the rollback untouched even by a mistake.
3. Start nothing else. Run, with the new binary and its real data directory and config:
   `mentat migrate-legacy --from <scratch copy>`. It refuses a target that already holds data, so a second run
   cannot double-load. It prints a table of counts and digests, the full-text and integrity proofs, the schedule
   result and the final file sizes, and exits non-zero on any mismatch (the new file is then left empty).
4. Start the new service.
5. **Immediately open the sign-in setup** (`GET /api/setup` says `setup_required: true`) and create the owner:
   until that is done, setup is open to whoever reaches the loopback port first. Then sign in.
6. Run the gating checks (section 4). If any fails, roll back (section 7).
7. Switch the clients (section 6).

## 4. Checks that gate the switch

- `migrate-legacy` ended with exit code 0 and every table line says `equal` (row count and content digest of typed
  values, with BLOBs byte for byte); three full-text indexes return the same rowids for 25 fixed queries each;
  `integrity_check` ok; autoincrement counters at or above the source; the only foreign-key violation is the one
  carried orphan; the report shows `WAL 0 bytes`.
- `curl http://127.0.0.1:8081/healthz` returns 200 `{"status":"ok"}`; an unauthenticated `GET /api/repos` returns 401;
  a request with an unknown `Host` header returns 403.
- Signed in: a repo list read returns the migrated repos; one write (add a tag) is visible on the next read.
- `GET /api/events` returns the history (including the orphan's neighbours); `mentat schedule list` shows the sweep
  schedule with the same id, cron expression and retry settings; one `mentat schedule trigger <id>` runs through the
  queue worker and ends `failed` with a clear error or `completed`, and the service stays up.
- An MCP client calls `repo_context` and `knowledge_query` and gets results.
- After the first scheduled 5-minute boundary a new job appears.

## 5. What was measured (copy of the rehearsal master)

- Source: 738,979,840 bytes, 26 tables copied plus 1 schedule moved. Run 1: 22.6 s. Run 2: 14.1 s. Target: 362,856,448 bytes,
  0-byte WAL. Largest tables: relationships 703,936 rows; job_events 127,679; jobs 31,928; symbols 22,184; embeddings
  13,776 (vectors byte-identical); code_references 1,268; findings 360; repos 169.
- Full-text hits identical by rowid: findings 214, symbols 1,073, code_references 580.
- Serve on the migrated copy: sign-in setup, authenticated reads, CLI writes while serve was running, a job through the
  queue worker, the scheduler firing on its own at the next 5-minute boundary, SIGTERM exit code 0, integrity ok.
- The source file's hash was unchanged after every run.

**Foreign keys.** The copy runs with foreign keys off so it can carry the orphan; the ported app writes through one
connection and it enforces them (`TestForeignKeysAreEnforcedOnWrites`), so new writes cannot add orphans. The orphan
does not make any normal operation fail (start, event reads and a job run were exercised with it present).

**Schedules and job history.** The one schedule moved into the scheduler module's tables with every field kept
(name, repo, kind, cron as written, payload, enabled, retry settings, created/updated, last run; the old stored
`next_run_at` is kept in the payload and the shown one is recomputed, as the old engine did at every start). All 31,928
job rows keep their schedule id (the column is now a plain label, not a foreign key). Cron: the scheduler reads five
fields; the old `0 */5 * * * *` is the same schedule (next fire times proved equal to the old engine's). Ticks missed
while the service is down are skipped, as before. Cron fields are read in `mentat.schedule_timezone` (default UTC; the
host is UTC).

## 6. Clients: every caller of the old surface

| Client | What changes | Who |
|---|---|---|
| Sigil frontend | sign in through identity (cookie or API key); send `X-Requested-With` on writes; read flat `{code,message}` errors; no trailing-slash paths; CORS is gone (same-origin or `identity.allowed_origins`) | Sigil owner (task CW-20261112-0086 is the Sigil port; confirm it covers this) |
| Deploy health check (`deploy.yaml`) | probe is now `/healthz` expecting 200 (already changed in this branch); the pre-deploy backup path and the restart unit follow the new data directory | driver / fleet tool owner |
| Cerberus descriptor (`stack-explorer.cerberus.yaml`) | command is `mentat serve` without `--port` (the port comes from `httphost.addr`); new working directory; the file still carries the old name | Cerberus owner |
| systemd user unit `mentat` | drop `--db`; set `MENTAT_DATA_DIR` (or rely on the user data directory) and `SUBSTRATE_CONFIG` | host owner |
| MCP clients (agents) | HTTP MCP is on the main listener at `/mcp` (port 8090 is gone) and needs a credential. Stdio (`mentat mcp`) needs a token file (`mcp.stdio_token_file`) holding an **identity API key** (`POST /api/identity/keys`, the `key_...` value, with an expiry the owner chooses): not a token from `POST /api/token`, which lives 15 minutes by default and at most an hour. Local filesystem trust and an offline bootstrap command are future framework changes. Scopes each key needs are listed below | the owners of each agent's MCP config |
| Scripts in `scripts/`, Hadron blueprint | use the CLI; none passes `--db` | Mentat |
| Operators | `STACK_EXPLORER_*`, `SE_EMBED_*`, `--db`, `--token` are gone; see the old-to-new table in `docs/conversion-notes.md` | operator |

**Scopes per MCP client** (issue keys no wider than this; every key needs `mentat.read` to reach `/mcp` at all):
- reads only (`repo_context`, `prior_art_*`, `symbol_lookup`, `graph_neighbors`, `knowledge_query`, `audit_show`, event tools): `mentat.read`.
- also records or updates findings (`finding_add`, `finding_update_status`): `mentat.read` + `mentat.write`.
- also imports or exports audits (`audit_import`, `audit_export`, which touch server paths): `mentat.read` + `mentat.admin` (plus `mentat.write` if it also writes findings).

**Tether gateway.** This repository serves no Mentat tool through the Tether gateway and configures none; the MCP endpoint
is Mentat's own (`/mcp`, and stdio). If the gateway is later pointed at it, that is a client of the list above and needs its own key.

Agents or scripts that import audits (`audit_import`, `audit_export`) now need the `mentat.admin` scope: they read and
write server paths. `GET /api/db/backup` also needs it (it streams the whole database).

## 7. Rollback

The old file is never written by the new binary and `migrate-legacy` opens it read-only. To roll back: stop the new
service, start the previous release against the old data directory (the fleet tool keeps it), and restore the
client changes already made. Anything written to the new file after the cutover is not in the old one; the freeze
window in section 2 is what keeps that loss at zero if the rollback happens before real use. Keep the old file until
the owner is satisfied.

## 8. Approval

The owner approves this plan separately from the pull request. Until then nothing is deployed and the live service
and database are not touched.
