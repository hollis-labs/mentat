# Cutover plan: Mentat onto Substrate

Status: DRAFT for the owner's approval. Nothing here has been run against the live service or its database; every
number comes from two rehearsals on a copy.

## 1. What this is and what the owner decides

Mentat runs today as one service (`mentat serve`, loopback port 8081) over one SQLite file of about 739 MB, and is in use.
The port replaces the binary and moves the data into a new file with a new layout (the framework's identity, queue
and scheduler tables sit beside Mentat's own). **No data is lost**: every row is copied, proved equal, and the old
file stays untouched as the rollback.

The owner decided on 2026-11-12:
1. **The failing git-history sweep is removed at cutover** and re-added when the repo paths are fixed. In production
   28,961 of 31,928 sweep jobs failed (last success 2026-05-14): the sweep runs `git log` in each repo's recorded path and
   that path does not exist on the host. The migration still carries the schedule across with every field; removing it
   is a recorded operator action after a faithful migration (runbook step 6). The 31,928 job rows and their events are
   history and stay exactly as migrated.
2. **The orphan row stays as it is.** The old database holds one `job_events` row (rowid 14) whose job no longer
   exists. It is carried over unchanged and left alone.
3. **The driver coordinates the client changes** (section 6): the Cerberus descriptor and unit through the Cerberus
   agent, MCP client configs through Agent Ops, Sigil's frontend calls through the Sigil port, and brings the owner one
   go or no-go.
4. **Timing.** The cutover happens after the code review and after the pull request is marked ready. The framework's
   offline identity bootstrap now exists (`mentat identity setup`, `mentat identity key create`), so the owner and the
   client keys are created before the service starts and first-run setup is never open over HTTP (`POST /api/setup`
   is always refused).

## 2. What stops, and for how long

Only the service. During the cutover Mentat's REST API, MCP endpoint, background scan worker and schedule are down.
Expected window: about **2 minutes** (stop the service, run the copy, start the new one), plus the time to sign in
and verify. Rehearsal timings: the copy takes **23 to 40 seconds** (three runs at head c24d0d8, with the SQL-level proof: 39.9 s, 27.0 s, 23.1 s; the first run reads a cold file; before that proof it was 14 to 23 s) for the 739 MB source. A freeze matters: anything
written to the old file after the copy starts is not in the new one, so the service is stopped before the copy.

## 3. Runbook

Prerequisites: the new release built by CI and installed beside the previous one (the fleet tool keeps the previous
release); free disk of at least **1.5 times** the old file (the new file ends at about 363 MB after the copy; the
copy itself needs more transiently); a backup of the old file (`deploy.yaml` already takes one before deploy).

0. **Step zero: the live data directory holds only the live files.** List the real data directory (`ls -la
   ~/.local/share/mentat`). It must contain `stack-explorer.db` and its `-wal` / `-shm` and nothing else. If anything
   else is there (above all `identity-signing.key`, `identity-signing.key.jwks.json`, `mentat.db`, `secrets/`), **stop
   and do not proceed**: a process of the new build has run there by mistake (even `mentat --help` creates a signing
   key and a database), and a key made by a stray run would be picked up as the service's key. Move the files aside,
   untouched, and find out which run made them. The same check is repeated after step 3 and before step 5, now expecting
   the three new files and the three old ones and nothing else. Until the cutover, the new binary is never run by hand on
   the host except through `scripts/mentat-run` with a scratch `MENTAT_DATA_DIR`.
1. Stop the service.
2. Copy the old file to a scratch name (`sqlite3 <old> ".backup <scratch>"`). The copy command never opens the old
   file for writing, but the scratch copy keeps the rollback untouched even by a mistake.
3. Start nothing else. Run, with the new binary and its real data directory and config:
   `mentat migrate-legacy --from <scratch copy>`. It refuses a target that already holds data, so a second run
   cannot double-load. It prints a table of counts and digests, the full-text and integrity proofs, the schedule
   result and the final file sizes, and exits non-zero on any mismatch (the new file is then left empty).
   **If the copy is killed** (kill -9, power loss) after its schedule step and before its commit, the new file holds
   scheduler records and no data. The next `migrate-legacy` refuses with a message saying so. Recovery: stop the
   service, delete the new database file and its `-wal` / `-shm` files, start the new binary once so the file and
   schema are created again (or run `mentat repo list`), and run step 3 again from the scratch copy. The old file and
   the scratch copy are not touched by any of this. (A copy that fails with an error undoes its schedule records
   itself; only a killed process leaves them.)
   **Order surprise:** `migrate-legacy` runs after the framework has started, so the database module has already
   created the new file and its schema, and the identity module has already created its signing key, before the copy
   begins. A key file in the data directory before the migration ran is expected. The copy reads only from the
   scratch `.backup` copy made in step 2, never from the live old file.
4. **Bootstrap, offline (no service running).** With the same data directory and config:
   - `mentat identity setup --username <name>` creates the owner. The password is read from a no-echo prompt (or the
     first line of stdin), never from an argument or the environment. It refuses if an owner exists.
   - `mentat identity key create --name <client> --scope <scopes> --ttl <duration> --out <new file>` once per client
     kind (scopes in section 6; `--out` writes a new 0600 file, or `--stdout` prints the key and nothing else). A key
     is shown once.
5. Start the new service, then check `GET /api/setup` answers `{"setup_over_http":false,"setup_required":false}` and
   that `POST /api/setup` answers 403. Sign in with the owner.
6. **Remove the migrated sweep schedule** (owner decision 1). First record its definition with
   `mentat schedule list --format json` into the cutover log (the repo id and schedule id are read from there; the
   history rows keep the old schedule id). Then `mentat schedule remove <schedule-id>`. Confirm `mentat schedule list`
   is empty. To re-add it later with the same definition, once the repo paths are fixed:
   ```
   mentat schedule add --name cw-git-history --repo <repo id from the log> --kind git-history-sweep \
     --cron "0 */5 * * * *" --payload '{}' --max-attempts 1 --retry-backoff fixed --retry-delay 1
   ```
   The definition is: name `cw-git-history`; kind `git-history-sweep`; cron `0 */5 * * * *` (every 5 minutes; the
   scheduler reads it as `*/5 * * * *`); payload `{}`; `max_attempts` 1; retry `fixed`, delay 1 second; enabled.
   A re-added schedule gets a new id; old job history keeps the old one.
7. Run the gating checks (section 4). If any fails, roll back (section 7). Then switch the clients (section 6).

## 4. Checks that gate the switch

- **Precondition, checked before step 1:** the host's time zone equals `mentat.schedule_timezone` (default `UTC`). The old
  engine read cron fields in the server's local time; the new scheduler reads them in the configured zone. Run
  `timedatectl show -p Timezone --value` on the host: it must print `Etc/UTC` or `UTC` while the setting is `UTC`
  (it was `Etc/UTC` on the production host when checked on 2026-11-12). If the host is in another zone, set
  `mentat.schedule_timezone` to that zone first, or every schedule fires at a shifted time.
- `migrate-legacy` ended with exit code 0 and every table line says `equal` (row count and content digest of typed
  values, with BLOBs byte for byte); three full-text indexes return the same rowids for 25 fixed queries each;
  `integrity_check` ok; autoincrement counters at or above the source; the only foreign-key violation is the one
  carried orphan; the report shows `WAL 0 bytes`.
- `GET /api/setup` says `setup_required` false (the owner was created offline in step 4); `POST /api/setup` is 403.
- `curl http://127.0.0.1:8081/healthz` returns 200 `{"status":"ok"}`; an unauthenticated `GET /api/repos` returns 401;
  a request with an unknown `Host` header returns 403.
- Signed in: a repo list read returns the migrated repos; one write (add a tag) is visible on the next read.
- `GET /api/events` returns the history (including the orphan's neighbours). Before step 6 removes it, `mentat schedule
  list` showed the migrated sweep schedule with its original id, cron expression and retry settings (recorded in the log).
- An MCP client calls `repo_context` and `knowledge_query` and gets results.
- Scheduler proof with a throwaway schedule (the migrated sweep is already removed, so it cannot fire): create
  `mentat schedule add --name cutover-check --repo <any repo id> --kind git-history-sweep --cron "* * * * *" --payload '{}'`
  (that kind only runs `git log`), wait past the next minute, confirm a job for the new schedule id appears
  (`GET /api/events?schedule_id=<id>`) and the service stays up, then `mentat schedule remove <id>`. Wait another
  minute and confirm no further job appears for either schedule.

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

**Whole-plan rehearsal at Substrate d453ac8 (2026-11-12, scratch copy, temporary data directory, throwaway password from
stdin).** Backup copy 0.8 s; `migrate-legacy` 26.4 s (all proofs as above, WAL 0, 362,872,832 bytes); `identity setup`
0.2 s and a second run refused; three keys (read, read+write, read+admin) created offline; `serve` healthy after 0.2 s;
`GET /api/setup` `setup_required` false, `POST /api/setup` 403; unauthenticated read 401, unknown `Host` 403, sign-in 200,
2 repos listed; read key: GET 200, POST 403; admin key reaches `/api/db/backup` (200); `/mcp` with the read+write key
initialized and listed 13 tools; `mentat mcp` under local trust (no token) listed the same tools while serve ran; the
migrated sweep schedule removed, a throwaway schedule fired within the minute (4 events) and was removed; SIGTERM:
exit 0 in 0.01 s, also with an event stream open; `integrity_check` ok; no panic in the log. `mentat schedule list
--format json` prints `null` when there are no schedules.

## 6. Clients: every caller of the old surface

| Client | What changes | Who |
|---|---|---|
| Sigil frontend | sign in through identity (cookie or API key); send `X-Requested-With` on writes; read flat `{code,message}` errors; no trailing-slash paths; CORS is gone (same-origin or `identity.allowed_origins`) | Sigil owner (task CW-20261112-0086 is the Sigil port; confirm it covers this) |
| Deploy health check (`deploy.yaml`) | probe is now `/healthz` expecting 200 (already changed in this branch); the pre-deploy backup path and the restart unit follow the new data directory | driver / fleet tool owner |
| Cerberus descriptor (`stack-explorer.cerberus.yaml`) | command is `mentat serve` without `--port` (the port comes from `httphost.addr`); new working directory; the file still carries the old name | Cerberus owner |
| systemd user unit `mentat` | drop `--db`; set `MENTAT_DATA_DIR` (or rely on the user data directory) and `SUBSTRATE_CONFIG` | host owner |
| MCP clients (agents) | HTTP MCP is on the main listener at `/mcp` (port 8090 is gone) and needs a credential. Stdio (`mentat mcp`): with no token file it runs under **local trust** as `local:operator` with the scopes `mentat.read` and `mentat.write` (set in code, not config; it prints a notice on stderr); anything that needs `mentat.admin` (`audit_import`, `audit_export`) must give a token file (`mcp.stdio_token_file`) holding an **identity API key** with that scope, made by `mentat identity key create` (not a token from `POST /api/token`, which lives at most an hour). A token that is given is always verified. Scopes each key needs are listed below | the owners of each agent's MCP config |
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

**Fixing the running version before the cutover.** `main` holds the Substrate code since PR #15 merged (merge commit
`0dc877294d97c209fa4e183b23b20bfda1eb7ff1`). The release running today is the pre-Substrate code, and a fix to it
has to be cut from the last commit of `main` before that merge: **`9152244561450d521c69fe858afff328bcf080c8`**
(docs: record v0.1.0 in changelog, #14). Branch from that commit, not from `main`; the same applies to a rollback
build. The release running on the server host is `v0.1.0-2-gae87349`, and `9152244` differs
from `ae87349` by one docs-only commit (#14: README, SECURITY, changelog), so a build from `9152244` is the running code.

## 8. Approval

The owner approves this plan separately from the pull request. Until then nothing is deployed and the live service
and database are not touched.
