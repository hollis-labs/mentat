# Cutover plan: Mentat onto Substrate

Status: **DONE on 2026-11-12 (17:56 to 17:58 UTC)**, run by the driver on the server host with the build of `main` at
`4c76279`, following runbook steps 0 to 7. The owner said go. The rest of this document is the plan as it was run; section 5
holds the rehearsal numbers it was approved on.

What the run showed:
- Downtime 66 seconds. `migrate-legacy` 23.8 s: all 26 tables equal (count and digest), the three full-text proofs, integrity ok,
  the one orphan carried, WAL 0 bytes.
- The owner was created offline; the installer deployed, applied the drop-in and reloaded systemd; the installed binary equals
  the binary used for the migration (same version string and sha256).
- Setup closed (`setup_required` false), 401 without a credential, 403 for an unknown `Host`, sign-in 200, repos and events read.
- The migrated sweep schedule was recorded and removed; a throwaway schedule fired once at the minute and was removed, and
  nothing fired after it.
- **Not done:** a tag write and an MCP client call (no live client exists; section 6).
- One defect found: the build reported `...+dirty`. Cause: the shared build workflow writes `dist/` inside the checkout before
  `go build`, and an untracked file makes Go stamp `vcs.modified=true`. Fixed by ignoring `/dist/` (`.gitignore`).
- Still open, the owner's call: removing the old inactive `stack-explorer.service` unit (section 6).

## 1. What this is and what the owner decides

Mentat runs today as one service (`mentat serve`, loopback port 8081) over one SQLite file of about 739 MB, and is in use.
The port replaces the binary and moves the data into a new file with a new layout (the framework's identity, queue
and scheduler tables sit beside Mentat's own). **No data is lost**: every row is copied, proved equal, and the old
file stays untouched as the rollback.

The owner decided on 2026-11-12:
1. **The failing git-history sweep is removed at cutover** and re-added when the repo paths are fixed. In production
   28,961 of 31,928 sweep jobs failed (last success 2026-05-14): the sweep runs `git log` in each repo's recorded path and
   that path does not exist on the host. The migration still carries the schedule across with every field; removing it
   is a recorded operator action after a faithful migration (runbook step 7). The 31,928 job rows and their events are
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

Prerequisites: the new release built by CI (a green run of the `build` workflow whose artifact the installer can fetch); free
disk of at least **1.5 times** the old file (the new file ends at about 363 MB after the copy; the copy itself needs more
transiently); the host's time zone equals `mentat.schedule_timezone` (section 4). **The installer comes last.** With the
new `serve` refusing a data directory that holds only the old database, `fleet-install deploy` cannot be the first step:
its restart would refuse and its health poll would fail. The order is: stop the unit, fetch the new binary *without
installing it*, migrate and bootstrap with that binary, then deploy.

The old unit, for reference (it stays in place and is the rollback): `ExecStart=%h/.local/bin/mentat --db
%h/.local/share/mentat/stack-explorer.db serve --host 127.0.0.1 --port 8081`, `EnvironmentFile=%h/.config/mentat/env`.
The new build takes none of those flags. `deploy.yaml` carries a drop-in (`mentat.service.d/20-fleet-exec.conf`) that
resets `ExecStart` to `%h/.local/bin/mentat serve`, resets `EnvironmentFile=` (empty: the old env file is no longer read)
and sets `SUBSTRATE_CONFIG=%h/.config/mentat/mentat.yaml`, so the deploy writes it and a rollback removes it. The listen
address is the manifest's `127.0.0.1:8081`, so the port needs no setting.

**Where the old environment goes.** The old build read these variable names: `STACK_EXPLORER_API_TOKEN`,
`STACK_EXPLORER_ACTOR_ID`, `STACK_EXPLORER_MODEL`, `STACK_EXPLORER_SESSION_ID`, `STACK_EXPLORER_MCP_ACTOR_KIND`,
`STACK_EXPLORER_MCP_ACTOR_ID`, `STACK_EXPLORER_MCP_MODEL`, `SE_EMBED_{SMALL,MEDIUM,FULL}_{PROVIDER,MODEL}`,
`SE_EMBED_OPENAI_BASE_URL`, `SE_EMBED_OPENAI_API_KEY`, `OPENAI_API_KEY`, `GITHUB_TOKEN`, and `USER`. **The new build reads
none of them from the environment.** Each one that is set in `~/.config/mentat/env` goes to the new config file
`~/.config/mentat/mentat.yaml` (mode 600, references only, never a key) or is dropped:
- `STACK_EXPLORER_API_TOKEN`: dropped (identity replaces it; clients get keys in step 4).
- `SE_EMBED_{SMALL,MEDIUM,FULL}_{PROVIDER,MODEL}`, `SE_EMBED_OPENAI_BASE_URL`: `mentat.embed.small|medium|full.provider|model`
  and `mentat.embed.openai_base_url` (the defaults are the old defaults; set only what the env file overrides).
- `SE_EMBED_OPENAI_API_KEY` or `OPENAI_API_KEY`: the value is moved, file to file and never printed or put in a command
  line, into a new 0600 file (for example `~/.config/mentat/openai-key`); the config holds `mentat.embed.openai_key_ref:
  file:///home/<user>/.config/mentat/openai-key`.
- `GITHUB_TOKEN`: same, into `~/.config/mentat/github-token`, with `mentat.github_token_ref: file:///home/<user>/.config/mentat/github-token`.
- `STACK_EXPLORER_ACTOR_ID`, `STACK_EXPLORER_MODEL` (and the `MCP_*` variants): `mentat.actor.id` and `mentat.actor.model`
  where the owner wants a fixed actor; the verified principal is used otherwise. `STACK_EXPLORER_SESSION_ID`, `USER`: dropped.
**What is actually set (read on the host on 2026-11-12, names only, no value printed):** the env file sets exactly one
variable, `STACK_EXPLORER_API_TOKEN` (64 characters). None of the embedding, OpenAI, GitHub or actor names is set. So
`mentat.yaml` needs nothing from it: no key moves, no secret files, and embeddings stay as they are today (no key
configured). The config file holds only what differs from the manifest, which may be nothing; **it must still exist**
(mode 600, possibly empty: the drop-in names it, and an empty file loads cleanly, checked), so step 3 creates it. Re-check
the names in the env file just before the cutover.
The env file itself is left as it is until the owner is satisfied (it is the rollback's configuration).

0. **Step zero: the live data directory holds only the live files.** List the real data directory (`ls -la
   ~/.local/share/mentat`). It must contain `stack-explorer.db` and its `-wal` / `-shm` and nothing else. If anything
   else is there (above all `identity-signing.key`, `identity-signing.key.jwks.json`, `mentat.db`, `secrets/`), **stop
   and do not proceed**: a process of the new build has run there by mistake (even `mentat --help` once created a signing
   key and a database), and a key made by a stray run would be picked up as the service's key. Move the files aside,
   untouched, and find out which run made them. The same check is repeated after step 4, now expecting the three old
   files, `mentat.db` (with `-wal` / `-shm` while open), `identity-signing.key` and `identity-signing.key.jwks.json`,
   and nothing else. Until the cutover, the new binary is never run on the host by hand except through
   `scripts/mentat-run` with a scratch `MENTAT_DATA_DIR`; the cutover's own runs of it in steps 3 and 4 are the one
   planned exception. (The binary now creates nothing for help, version, config, health or the data commands, and
   refuses `serve`, `mcp`, `identity setup` and the data commands in a directory that holds only `stack-explorer.db`,
   saying to run `migrate-legacy` first; only `serve`, `migrate-legacy`, `identity setup` and `identity key create`
   create files, and `mcp` needs an existing database.)
1. Stop the unit: `systemctl --user stop mentat` (the old binary; it closes its database cleanly). From here the service
   is down until the restart in step 5.
2. Copy the old file to a scratch name **outside the data directory**, for example
   `mkdir -p ~/.local/state/mentat-cutover && sqlite3 ~/.local/share/mentat/stack-explorer.db ".backup
   ~/.local/state/mentat-cutover/stack-explorer.scratch.db"`. The copy command never opens the old file for writing, but
   the scratch copy keeps the rollback untouched even by a mistake. Record its sha256.
3. **Fetch the new binary without installing it.** Download the CI artifact for the commit to deploy
   (`gh run download <run-id> -R hollis-labs/mentat -n mentat-<sha12> -D ~/.local/state/mentat-cutover/artifact`).
   Do not rely on the installer's cache (`~/.local/state/fleet-install/cache/mentat/run-<run id>/`) for the linux binary:
   after a dry run the newest directory held only the darwin tarball. Check the tarball against `SHA256SUMS` (`sha256sum -c`), extract `mentat` into
   `~/.local/state/mentat-cutover/bin/`, and record `./mentat version` and `sha256sum mentat`. Step 6 compares both with
   what the installer then installs: they must be identical, or stop.
   Write `~/.config/mentat/mentat.yaml` (and the secret files) from the list above, mode 600, **before** the next step.
   Then run, with that binary, the real data directory (the default, `~/.local/share/mentat`) and
   `SUBSTRATE_CONFIG=~/.config/mentat/mentat.yaml`: `mentat migrate-legacy --from
   ~/.local/state/mentat-cutover/stack-explorer.scratch.db`. It refuses a target that already holds data, so a second run
   cannot double-load. It prints a table of counts and digests, the full-text and integrity proofs, the schedule
   result and the final file sizes, and exits non-zero on any mismatch (the new file is then left empty).
   **If the copy is killed** (kill -9, power loss) after its schedule step and before its commit, the new file holds
   scheduler records and no data. The next `migrate-legacy` refuses with a message saying so. Recovery: delete the new
   database file and its `-wal` / `-shm` files, and run step 3 again from the scratch copy (the binary creates the file
   and schema when `migrate-legacy` starts). The old file and the scratch copy are not touched by any of this. (A copy
   that fails with an error undoes its schedule records itself; only a killed process leaves them.)
   **Order surprise:** `migrate-legacy` runs after the framework has started, so the database module has already
   created the new file and its schema, and the identity module has already created its signing key, before the copy
   begins. A key file in the data directory before the migration ran is expected. The copy reads only from the
   scratch `.backup` copy made in step 2, never from the live old file.
4. **Bootstrap, offline (no service running).** With the same binary, data directory and config:
   - `mentat identity setup --username <name>` creates the owner. The password is read from a no-echo prompt (or the
     first line of stdin), never from an argument or the environment. It refuses if an owner exists.
   - `mentat identity key create --name <client> --scope <scopes> --ttl <duration> --out <new file>` once per client
     kind (scopes in section 6; `--out` writes a new 0600 file, or `--stdout` prints the key and nothing else). A key
     is shown once.
   Then repeat the directory check of step zero (the expected list is in step 0).
5. **Deploy:** `fleet-install deploy mentat --commit <sha>`. Its preflight backs up the migrated `mentat.db`, it installs the
   release beside the previous ones, flips `~/.local/bin/mentat`, writes the drop-in, restarts `mentat.service` and polls
   `/healthz`. The plan does not rely on the installer reloading systemd: right after the restart (or with the installer's
   restart-done flow) compare `systemctl --user cat mentat` with the drop-in in `deploy.yaml`; if `ExecStart` is not
   `%h/.local/bin/mentat serve`, stop the service, `systemctl --user daemon-reload` and start it again. The preflight
   backs up `mentat.db`, not the old file, on purpose: the migrated file exists before the deploy on the first run and on
   every later one, and the old file's rollback is the untouched original plus the scratch copy of step 2.
6. **Same build check.** `~/.local/bin/mentat version` and `sha256sum` of the installed binary (the symlink's target) equal
   the values recorded in step 3. Then check `GET /api/setup` answers `{"setup_over_http":false,"setup_required":false}`
   and that `POST /api/setup` answers 403. Sign in with the owner. If any of this fails, stop the unit and roll back (section 7).
7. **Remove the migrated sweep schedule** (owner decision 1). First record its definition with
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
8. Run the gating checks (section 4). If any fails, roll back (section 7). Then switch the clients (section 6).

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
- `GET /api/events` returns the history (including the orphan's neighbours). Before step 7 removes it, `mentat schedule
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
exit 0 in 0.01 s, also with an event stream open; `integrity_check` ok; no panic in the log. (At that rehearsal `mentat schedule list --format json` printed `null` for no schedules; it now prints `[]`.)

## 6. Clients: every caller of the old surface

**No live client was found (2026-11-12).** The live service's log for the last seven days shows three HTTP requests, all
from the driver that day (two `GET /api/repos`, one `GET /healthz`); no MCP client configuration on the host names Mentat,
and the Tether gateway's catalog has no Mentat server. The only thing that has used the service is its own sweep schedule.
So the client switch is empty in practice today; the table below stays as the contract for whoever connects next. There
is also an old, disabled and inactive unit `stack-explorer.service` pointing at the same port with the old binary name:
remove it after the cutover (the owner's call, not part of the runbook).

| Client | What changes | Who |
|---|---|---|
| Sigil frontend | sign in through identity (cookie or API key); send `X-Requested-With` on writes; read flat `{code,message}` errors; no trailing-slash paths; CORS is gone (same-origin or `identity.allowed_origins`) | Sigil owner (task CW-20261112-0086 is the Sigil port; confirm it covers this) |
| Deploy health check (`deploy.yaml`) | probe is now `/healthz` expecting 200 (already changed in this branch); the pre-deploy backup path and the restart unit follow the new data directory | driver / fleet tool owner |
| Cerberus descriptor (`stack-explorer.cerberus.yaml`) | command is `mentat serve` without `--port` (the port comes from `httphost.addr`); new working directory; the file still carries the old name | Cerberus owner |
| systemd user unit `mentat` | the deploy writes a drop-in (`20-fleet-exec.conf`) that resets `ExecStart` to `mentat serve`, clears `EnvironmentFile` and sets `SUBSTRATE_CONFIG`; the old unit file and env file stay in place for the rollback | `deploy.yaml` (driver runs the installer) |
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

The old file is never written by the new binary and `migrate-legacy` opens it read-only; the old unit file and env file are
not edited. To roll back: stop the unit (`systemctl --user stop mentat`); flip the link back with
`fleet-install rollback mentat` (it points `~/.local/bin/mentat` at the previous release, `v0.1.0-2-gae87349`);
remove the drop-in `~/.config/systemd/user/mentat.service.d/20-fleet-exec.conf` and run `systemctl --user daemon-reload`,
so the unit's own `ExecStart` (`--db ... stack-explorer.db serve`) and `EnvironmentFile` apply again; start the unit. It runs
the old binary on the old file, which was never written. **Not confirmed:** whether `fleet-install rollback` removes the
drop-in itself (the Loom plan removes it by hand for the first cutover); until a dry run shows it, remove it by hand as
written. Then restore the client changes already made. Anything written to the new file after the cutover is not in the old
one; the freeze window in section 2 is what keeps that loss at zero if the rollback happens before real use. Keep the old
file, the scratch copy and the env file until the owner is satisfied.

**Fixing the running version before the cutover.** `main` holds the Substrate code since PR #15 merged (merge commit
`0dc877294d97c209fa4e183b23b20bfda1eb7ff1`). The release running today is the pre-Substrate code, and a fix to it
has to be cut from the last commit of `main` before that merge: **`9152244561450d521c69fe858afff328bcf080c8`**
(docs: record v0.1.0 in changelog, #14). Branch from that commit, not from `main`; the same applies to a rollback
build. The release running on the server host is `v0.1.0-2-gae87349`, and `9152244` differs
from `ae87349` by one docs-only commit (#14: README, SECURITY, changelog), so a build from `9152244` is the running code.

## 8. Approval

The owner approves this plan separately from the pull request. Until then nothing is deployed and the live service
and database are not touched.
