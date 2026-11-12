-- The Mentat schema, squashed to one migration (clean break from the pre-Substrate layout). SQLite only.
--
-- It is the final shape of the 24 numbered migrations the app ran before moving onto Substrate,
-- plus the seed rows of review_dimensions, lenses and lens_dimensions that those migrations inserted.
-- `mentat migrate-legacy` copies a pre-Substrate database into a file created from this schema.
--
-- Not here, on purpose: accounts, passwords and sessions (the identity module's identity_* tables).
--
-- Until the first release on Substrate this is the ONLY migration: a schema change edits this file.
-- After the first release changes are new numbered files.

-- +goose Up

CREATE TABLE repos (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    url         TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    stack       TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT '',
    is_own      INTEGER NOT NULL DEFAULT 0,
    local_path  TEXT NOT NULL DEFAULT '',
    homepage    TEXT NOT NULL DEFAULT '',
    license     TEXT NOT NULL DEFAULT '',
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
, embedding_profile TEXT NOT NULL DEFAULT 'none');

CREATE INDEX idx_repos_category ON repos(category);

CREATE INDEX idx_repos_is_own ON repos(is_own);

CREATE INDEX idx_repos_stack ON repos(stack);

CREATE TABLE tags (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE repo_tags (
    repo_id TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    tag_id  TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (repo_id, tag_id)
);

CREATE INDEX idx_repo_tags_tag ON repo_tags(tag_id);

CREATE TABLE snapshots (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id         TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    captured_at     TEXT NOT NULL,
    stars           INTEGER,
    forks           INTEGER,
    open_issues     INTEGER,
    contributors    INTEGER,
    last_commit_at  TEXT,
    commits_30d     INTEGER,
    loc             INTEGER,
    files           INTEGER,
    test_files      INTEGER,
    dependencies    INTEGER,
    complexity_avg  REAL,
    raw_json        TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_snapshots_repo ON snapshots(repo_id, captured_at DESC);

CREATE TABLE review_dimensions (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    category    TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    weight      REAL NOT NULL DEFAULT 1.0,
    sort_order  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE scorecards (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id     TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    scored_at   TEXT NOT NULL,
    overall     REAL NOT NULL DEFAULT 0,
    notes       TEXT NOT NULL DEFAULT ''
, lens_id TEXT REFERENCES lenses(id) ON DELETE SET NULL);

CREATE INDEX idx_scorecards_repo ON scorecards(repo_id, scored_at DESC);

CREATE TABLE dimension_scores (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    scorecard_id  INTEGER NOT NULL REFERENCES scorecards(id) ON DELETE CASCADE,
    dimension_id  TEXT NOT NULL REFERENCES review_dimensions(id) ON DELETE CASCADE,
    score         REAL NOT NULL DEFAULT 0,
    evidence      TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_dim_scores_card ON dimension_scores(scorecard_id);

CREATE INDEX idx_dim_scores_dim ON dimension_scores(dimension_id);

CREATE UNIQUE INDEX idx_dim_scores_uniq ON dimension_scores(scorecard_id, dimension_id);

CREATE TABLE architecture_patterns (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL DEFAULT 'pattern',
    category    TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE repo_patterns (
    repo_id    TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    pattern_id TEXT NOT NULL REFERENCES architecture_patterns(id) ON DELETE CASCADE,
    quality    TEXT NOT NULL DEFAULT 'present',
    notes      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (repo_id, pattern_id)
);

CREATE INDEX idx_repo_patterns_pattern ON repo_patterns(pattern_id);

CREATE TABLE findings (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id     TEXT REFERENCES repos(id) ON DELETE SET NULL,
    title       TEXT NOT NULL,
    category    TEXT NOT NULL DEFAULT '',
    severity    TEXT NOT NULL DEFAULT 'info',
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'open',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
, audit_id INTEGER REFERENCES audits(id) ON DELETE SET NULL, body_markdown TEXT NOT NULL DEFAULT '', audited_at_ref TEXT, is_out_of_scope INTEGER NOT NULL DEFAULT 0, symbol_id INTEGER, actor_kind TEXT NOT NULL DEFAULT 'unknown', actor_id TEXT NOT NULL DEFAULT 'unknown', session_id TEXT, tool_name TEXT, model_name TEXT);

CREATE INDEX idx_findings_repo ON findings(repo_id);

CREATE INDEX idx_findings_category ON findings(category);

CREATE INDEX idx_findings_status ON findings(status);

CREATE TABLE comparison_sets (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE TABLE comparison_set_repos (
    set_id   TEXT NOT NULL REFERENCES comparison_sets(id) ON DELETE CASCADE,
    repo_id  TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    role     TEXT NOT NULL DEFAULT 'subject',
    PRIMARY KEY (set_id, repo_id)
);

CREATE INDEX idx_comp_set_repos_set ON comparison_set_repos(set_id);

CREATE TABLE lenses (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE TABLE lens_dimensions (
    lens_id      TEXT NOT NULL REFERENCES lenses(id) ON DELETE CASCADE,
    dimension_id TEXT NOT NULL REFERENCES review_dimensions(id) ON DELETE CASCADE,
    weight       REAL NOT NULL DEFAULT 1.0,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (lens_id, dimension_id)
);

CREATE INDEX idx_lens_dims_lens ON lens_dimensions(lens_id);

CREATE INDEX idx_scorecards_lens ON scorecards(repo_id, lens_id);

CREATE TABLE report_configs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    lens_id     TEXT REFERENCES lenses(id) ON DELETE SET NULL,
    audience    TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE TABLE report_config_filters (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    config_id   TEXT NOT NULL REFERENCES report_configs(id) ON DELETE CASCADE,
    filter_type TEXT NOT NULL,
    filter_value TEXT NOT NULL
);

CREATE INDEX idx_report_config_filters ON report_config_filters(config_id);

CREATE TABLE scans (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id       TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    blueprint     TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    result_json   TEXT NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT '',
    started_at    TEXT,
    finished_at   TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX idx_scans_repo ON scans(repo_id);

CREATE INDEX idx_scans_status ON scans(status);

CREATE INDEX idx_scans_blueprint ON scans(blueprint);

CREATE INDEX idx_scans_created ON scans(created_at DESC);

CREATE TABLE audits (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id          TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    scope            TEXT NOT NULL,
    scope_paths      TEXT NOT NULL DEFAULT '[]',
    audit_type       TEXT NOT NULL,
    auditor          TEXT NOT NULL DEFAULT '',
    audited_at_ref   TEXT,
    summary_markdown TEXT NOT NULL DEFAULT '',
    verdict          TEXT,
    status           TEXT NOT NULL DEFAULT 'in_progress',
    supersedes_id    INTEGER REFERENCES audits(id),
    actor_kind       TEXT NOT NULL,
    actor_id         TEXT NOT NULL,
    session_id       TEXT,
    tool_name        TEXT,
    model_name       TEXT,
    started_at       TEXT NOT NULL,
    finished_at      TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX idx_audits_repo ON audits(repo_id, status);

CREATE INDEX idx_audits_audited_at ON audits(repo_id, audited_at_ref);

CREATE INDEX idx_findings_audit ON findings(audit_id);

CREATE TABLE audit_themes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id     TEXT REFERENCES repos(id) ON DELETE SET NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE(repo_id, name)
);

CREATE TABLE finding_themes (
    finding_id INTEGER NOT NULL REFERENCES findings(id) ON DELETE CASCADE,
    theme_id   INTEGER NOT NULL REFERENCES audit_themes(id) ON DELETE CASCADE,
    PRIMARY KEY (finding_id, theme_id)
);

CREATE INDEX idx_finding_themes_theme ON finding_themes(theme_id);

CREATE TABLE symbols (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id          TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    kind             TEXT NOT NULL,
    name             TEXT NOT NULL,
    qualified_name   TEXT NOT NULL,
    file_path        TEXT NOT NULL,
    line_start       INTEGER,
    line_end         INTEGER,
    content_hash     TEXT NOT NULL,
    signature_hash   TEXT NOT NULL,
    parent_symbol_id INTEGER REFERENCES symbols(id) ON DELETE CASCADE,
    language         TEXT NOT NULL,
    visibility       TEXT,
    docstring        TEXT NOT NULL DEFAULT '',
    stale_since_commit TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX idx_symbols_repo_content_hash ON symbols(repo_id, content_hash);

CREATE INDEX idx_symbols_repo_kind ON symbols(repo_id, kind);

CREATE INDEX idx_symbols_repo_language ON symbols(repo_id, language);

CREATE INDEX idx_symbols_parent ON symbols(parent_symbol_id);

CREATE TABLE IF NOT EXISTS "code_references" (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id             TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    file_path           TEXT NOT NULL,
    line_start          INTEGER,
    line_end            INTEGER,
    description         TEXT NOT NULL DEFAULT '',
    ref_type            TEXT NOT NULL DEFAULT '',
    pattern_id          TEXT REFERENCES architecture_patterns(id) ON DELETE SET NULL,
    finding_id          INTEGER REFERENCES findings(id) ON DELETE SET NULL,
    symbol_id           INTEGER REFERENCES symbols(id) ON DELETE SET NULL,
    anchor_content_hash TEXT NOT NULL DEFAULT '',
    stale_since_commit  TEXT,
    created_at          TEXT NOT NULL
);

CREATE INDEX idx_code_refs_repo ON code_references(repo_id);

CREATE INDEX idx_code_refs_pattern ON code_references(pattern_id);

CREATE INDEX idx_code_refs_finding ON code_references(finding_id);

CREATE INDEX idx_code_refs_symbol ON code_references(symbol_id);

CREATE INDEX idx_code_refs_anchor_content_hash ON code_references(anchor_content_hash);

CREATE TABLE relationships (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id       TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    src_symbol_id INTEGER NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
    dst_symbol_id INTEGER NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,
    weight        REAL NOT NULL DEFAULT 1.0,
    source        TEXT NOT NULL,
    discovered_at TEXT NOT NULL,
    UNIQUE(src_symbol_id, dst_symbol_id, kind, source)
);

CREATE INDEX idx_relationships_repo_kind ON relationships(repo_id, kind);

CREATE INDEX idx_relationships_src_kind ON relationships(src_symbol_id, kind);

CREATE INDEX idx_relationships_dst_kind ON relationships(dst_symbol_id, kind);

CREATE INDEX idx_relationships_source_kind ON relationships(source, kind);

CREATE VIRTUAL TABLE findings_fts USING fts5(
    title,
    description,
    body_markdown,
    content='findings',
    content_rowid='id'
);

CREATE VIRTUAL TABLE symbols_fts USING fts5(
    name,
    qualified_name,
    file_path,
    docstring,
    content='symbols',
    content_rowid='id'
);

CREATE VIRTUAL TABLE code_references_fts USING fts5(
    file_path,
    description,
    content='code_references',
    content_rowid='id'
);

-- +goose StatementBegin
CREATE TRIGGER findings_fts_ai AFTER INSERT ON findings BEGIN
    INSERT INTO findings_fts(rowid, title, description, body_markdown)
    VALUES (new.id, new.title, new.description, new.body_markdown);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER findings_fts_ad AFTER DELETE ON findings BEGIN
    INSERT INTO findings_fts(findings_fts, rowid, title, description, body_markdown)
    VALUES ('delete', old.id, old.title, old.description, old.body_markdown);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER findings_fts_au AFTER UPDATE ON findings BEGIN
    INSERT INTO findings_fts(findings_fts, rowid, title, description, body_markdown)
    VALUES ('delete', old.id, old.title, old.description, old.body_markdown);
    INSERT INTO findings_fts(rowid, title, description, body_markdown)
    VALUES (new.id, new.title, new.description, new.body_markdown);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER symbols_fts_ai AFTER INSERT ON symbols BEGIN
    INSERT INTO symbols_fts(rowid, name, qualified_name, file_path, docstring)
    VALUES (new.id, new.name, new.qualified_name, new.file_path, new.docstring);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER symbols_fts_ad AFTER DELETE ON symbols BEGIN
    INSERT INTO symbols_fts(symbols_fts, rowid, name, qualified_name, file_path, docstring)
    VALUES ('delete', old.id, old.name, old.qualified_name, old.file_path, old.docstring);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER symbols_fts_au AFTER UPDATE ON symbols BEGIN
    INSERT INTO symbols_fts(symbols_fts, rowid, name, qualified_name, file_path, docstring)
    VALUES ('delete', old.id, old.name, old.qualified_name, old.file_path, old.docstring);
    INSERT INTO symbols_fts(rowid, name, qualified_name, file_path, docstring)
    VALUES (new.id, new.name, new.qualified_name, new.file_path, new.docstring);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER code_references_fts_ai AFTER INSERT ON code_references BEGIN
    INSERT INTO code_references_fts(rowid, file_path, description)
    VALUES (new.id, new.file_path, new.description);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER code_references_fts_ad AFTER DELETE ON code_references BEGIN
    INSERT INTO code_references_fts(code_references_fts, rowid, file_path, description)
    VALUES ('delete', old.id, old.file_path, old.description);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER code_references_fts_au AFTER UPDATE ON code_references BEGIN
    INSERT INTO code_references_fts(code_references_fts, rowid, file_path, description)
    VALUES ('delete', old.id, old.file_path, old.description);
    INSERT INTO code_references_fts(rowid, file_path, description)
    VALUES (new.id, new.file_path, new.description);
END;
-- +goose StatementEnd

CREATE TABLE embeddings (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    target_kind  TEXT NOT NULL,
    target_id    INTEGER NOT NULL,
    model        TEXT NOT NULL,
    dim          INTEGER NOT NULL,
    vector       BLOB NOT NULL,
    content_hash TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    UNIQUE(target_kind, target_id, model)
);

CREATE INDEX idx_emb_target ON embeddings(target_kind, target_id);

CREATE INDEX idx_emb_model ON embeddings(model);

CREATE TABLE jobs (
    id               TEXT PRIMARY KEY,
    -- The id of the schedule that fired the job. Schedules live in the scheduler module's tables (gosched_*), so this
    -- is a plain label that stays on the history row after the schedule is removed.
    schedule_id      TEXT,
    repo_id          TEXT REFERENCES repos(id) ON DELETE CASCADE,
    kind             TEXT NOT NULL,
    status           TEXT NOT NULL,
    attempt_count    INTEGER NOT NULL DEFAULT 0,
    max_attempts     INTEGER NOT NULL DEFAULT 1,
    retry_backoff    TEXT NOT NULL DEFAULT 'fixed',
    retry_delay_secs INTEGER NOT NULL DEFAULT 1,
    payload_json     TEXT NOT NULL DEFAULT '{}',
    output_json      TEXT NOT NULL DEFAULT '{}',
    error            TEXT NOT NULL DEFAULT '',
    started_at       TEXT,
    finished_at      TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX idx_jobs_schedule ON jobs(schedule_id);

CREATE INDEX idx_jobs_repo ON jobs(repo_id);

CREATE INDEX idx_jobs_kind ON jobs(kind);

CREATE INDEX idx_jobs_status ON jobs(status);

CREATE INDEX idx_jobs_created ON jobs(created_at DESC);

CREATE TABLE job_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id       TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    schedule_id  TEXT,
    repo_id      TEXT REFERENCES repos(id) ON DELETE CASCADE,
    job_kind     TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT '',
    message      TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL
);

CREATE INDEX idx_job_events_job ON job_events(job_id, id);

CREATE INDEX idx_job_events_schedule ON job_events(schedule_id, id);

CREATE INDEX idx_job_events_repo ON job_events(repo_id, id);

CREATE INDEX idx_job_events_kind ON job_events(job_kind, id);

CREATE UNIQUE INDEX idx_symbols_repo_file_qualified_name
ON symbols(repo_id, file_path, qualified_name);

CREATE INDEX idx_symbols_repo_qualified_name_lookup
ON symbols(repo_id, qualified_name);

CREATE INDEX idx_symbols_repo_file_content_hash
ON symbols(repo_id, file_path, content_hash);

-- seed: review_dimensions
INSERT INTO review_dimensions VALUES('hooks','Hooks','agent_patterns','Lifecycle hooks, enforcement hooks, extension hooks',1.0,1);
INSERT INTO review_dimensions VALUES('loops','Loops','agent_patterns','Agent loops, feedback loops, retry patterns',1.0,2);
INSERT INTO review_dimensions VALUES('tool_calls','Tool Calls','agent_patterns','Security, permissions, long-running, token efficiency',1.0,3);
INSERT INTO review_dimensions VALUES('agents','Agents','agent_patterns','Agent definition, coordination, delegation patterns',1.0,4);
INSERT INTO review_dimensions VALUES('skills','Skills','agent_patterns','Skill discovery, composition, reuse',1.0,5);
INSERT INTO review_dimensions VALUES('progressive_context','Progressive Context','agent_patterns','Progressive context discovery and loading',1.0,6);
INSERT INTO review_dimensions VALUES('memory','Memory','agent_patterns','Persistence, retrieval, decay patterns',1.0,7);
INSERT INTO review_dimensions VALUES('agent_coordination','Agent Coordination','agent_patterns','Multi-agent communication and handoff',1.0,8);
INSERT INTO review_dimensions VALUES('stack','Stack Details','code_quality','Language, framework, dependency choices',0.5,9);
INSERT INTO review_dimensions VALUES('loc','Codebase Size','code_quality','Lines of code, file count, growth rate',0.5,10);
INSERT INTO review_dimensions VALUES('security','Security Posture','project_health','Dependency scanning, secret management, input validation',1.0,11);
INSERT INTO review_dimensions VALUES('maturity','Maturity','project_health','Tests, docs, CI/CD, release cadence',1.0,12);
INSERT INTO review_dimensions VALUES('complexity','Complexity','code_quality','Cyclomatic, cognitive, architectural complexity',1.0,13);
INSERT INTO review_dimensions VALUES('multi_provider','Multi-Provider','features','Support for multiple LLM providers and models',1.0,14);
INSERT INTO review_dimensions VALUES('multi_modal','Multi-Modal','features','Support for text, image, audio, video, and other modalities',1.0,15);
INSERT INTO review_dimensions VALUES('ui_extensibility','UI Extensibility','features','Widgets, plugins, custom components, envelope/card systems',1.0,16);
INSERT INTO review_dimensions VALUES('enterprise','Enterprise Readiness','project_health','SSO, RBAC, audit logging, data governance, compliance controls',1.0,17);
INSERT INTO review_dimensions VALUES('data_governance','Data Governance','project_health','Data retention, redaction, PII handling, export controls',1.0,18);

-- seed: lenses
INSERT INTO lenses VALUES('agent-platform','Agent Platform','Full agent platform with tool calling, coordination, skills','2026-04-03T00:00:00Z');
INSERT INTO lenses VALUES('chat-app','Chat Application','GUI AI chat apps — tool calls, security, enterprise, features, UX','2026-04-03T00:00:00Z');
INSERT INTO lenses VALUES('automation','Automation / DAG','Workflow automation, DAG orchestration, scheduling','2026-04-03T00:00:00Z');
INSERT INTO lenses VALUES('memory-system','Memory System','Memory, RAG, embeddings, context management','2026-04-03T00:00:00Z');
INSERT INTO lenses VALUES('agent-framework','Agent Framework','Agent definition, skills, coordination, tool calling','2026-04-03T00:00:00Z');
INSERT INTO lenses VALUES('desktop-app','Desktop Application','Desktop/GUI apps — UX, stability, simplicity','2026-04-03T00:00:00Z');
INSERT INTO lenses VALUES('infra-tool','Infrastructure Tool','Service management, monitoring, ops tooling','2026-04-03T00:00:00Z');
INSERT INTO lenses VALUES('content-pipeline','Content Pipeline','Ingest, transform, and output content','2026-04-03T00:00:00Z');
INSERT INTO lenses VALUES('general','General','All dimensions equally weighted — default lens','2026-04-03T00:00:00Z');

-- seed: lens_dimensions
INSERT INTO lens_dimensions VALUES('agent-platform','hooks',1.0,1);
INSERT INTO lens_dimensions VALUES('agent-platform','loops',1.0,2);
INSERT INTO lens_dimensions VALUES('agent-platform','tool_calls',1.0,3);
INSERT INTO lens_dimensions VALUES('agent-platform','agents',1.0,4);
INSERT INTO lens_dimensions VALUES('agent-platform','skills',1.0,5);
INSERT INTO lens_dimensions VALUES('agent-platform','progressive_context',1.0,6);
INSERT INTO lens_dimensions VALUES('agent-platform','memory',1.0,7);
INSERT INTO lens_dimensions VALUES('agent-platform','agent_coordination',1.0,8);
INSERT INTO lens_dimensions VALUES('agent-platform','stack',0.5,9);
INSERT INTO lens_dimensions VALUES('agent-platform','loc',0.5,10);
INSERT INTO lens_dimensions VALUES('agent-platform','security',1.0,11);
INSERT INTO lens_dimensions VALUES('agent-platform','maturity',1.0,12);
INSERT INTO lens_dimensions VALUES('agent-platform','complexity',1.0,13);
INSERT INTO lens_dimensions VALUES('chat-app','tool_calls',1.5,1);
INSERT INTO lens_dimensions VALUES('chat-app','security',1.5,2);
INSERT INTO lens_dimensions VALUES('chat-app','enterprise',1.5,3);
INSERT INTO lens_dimensions VALUES('chat-app','data_governance',1.0,4);
INSERT INTO lens_dimensions VALUES('chat-app','multi_provider',1.5,5);
INSERT INTO lens_dimensions VALUES('chat-app','multi_modal',1.0,6);
INSERT INTO lens_dimensions VALUES('chat-app','ui_extensibility',1.5,7);
INSERT INTO lens_dimensions VALUES('chat-app','memory',1.0,8);
INSERT INTO lens_dimensions VALUES('chat-app','hooks',0.5,9);
INSERT INTO lens_dimensions VALUES('chat-app','agents',0.5,10);
INSERT INTO lens_dimensions VALUES('chat-app','skills',0.5,11);
INSERT INTO lens_dimensions VALUES('chat-app','stack',0.5,12);
INSERT INTO lens_dimensions VALUES('chat-app','maturity',1.0,13);
INSERT INTO lens_dimensions VALUES('chat-app','complexity',0.5,14);
INSERT INTO lens_dimensions VALUES('automation','hooks',1.0,1);
INSERT INTO lens_dimensions VALUES('automation','loops',1.5,2);
INSERT INTO lens_dimensions VALUES('automation','skills',1.5,3);
INSERT INTO lens_dimensions VALUES('automation','tool_calls',1.0,4);
INSERT INTO lens_dimensions VALUES('automation','memory',1.0,5);
INSERT INTO lens_dimensions VALUES('automation','security',1.0,6);
INSERT INTO lens_dimensions VALUES('automation','maturity',1.0,7);
INSERT INTO lens_dimensions VALUES('automation','complexity',1.0,8);
INSERT INTO lens_dimensions VALUES('automation','stack',0.5,9);
INSERT INTO lens_dimensions VALUES('automation','loc',0.5,10);
INSERT INTO lens_dimensions VALUES('memory-system','memory',2.0,1);
INSERT INTO lens_dimensions VALUES('memory-system','progressive_context',1.5,2);
INSERT INTO lens_dimensions VALUES('memory-system','tool_calls',1.0,3);
INSERT INTO lens_dimensions VALUES('memory-system','security',1.0,4);
INSERT INTO lens_dimensions VALUES('memory-system','maturity',1.0,5);
INSERT INTO lens_dimensions VALUES('memory-system','complexity',1.0,6);
INSERT INTO lens_dimensions VALUES('memory-system','stack',0.5,7);
INSERT INTO lens_dimensions VALUES('memory-system','loc',0.5,8);
INSERT INTO lens_dimensions VALUES('agent-framework','agents',1.5,1);
INSERT INTO lens_dimensions VALUES('agent-framework','skills',1.5,2);
INSERT INTO lens_dimensions VALUES('agent-framework','hooks',1.0,3);
INSERT INTO lens_dimensions VALUES('agent-framework','loops',1.0,4);
INSERT INTO lens_dimensions VALUES('agent-framework','tool_calls',1.5,5);
INSERT INTO lens_dimensions VALUES('agent-framework','agent_coordination',1.5,6);
INSERT INTO lens_dimensions VALUES('agent-framework','maturity',1.0,7);
INSERT INTO lens_dimensions VALUES('agent-framework','complexity',1.0,8);
INSERT INTO lens_dimensions VALUES('agent-framework','stack',0.5,9);
INSERT INTO lens_dimensions VALUES('desktop-app','memory',1.0,1);
INSERT INTO lens_dimensions VALUES('desktop-app','security',1.0,2);
INSERT INTO lens_dimensions VALUES('desktop-app','maturity',1.0,3);
INSERT INTO lens_dimensions VALUES('desktop-app','complexity',1.0,4);
INSERT INTO lens_dimensions VALUES('desktop-app','stack',1.0,5);
INSERT INTO lens_dimensions VALUES('desktop-app','loc',1.0,6);
INSERT INTO lens_dimensions VALUES('infra-tool','hooks',1.0,1);
INSERT INTO lens_dimensions VALUES('infra-tool','loops',1.0,2);
INSERT INTO lens_dimensions VALUES('infra-tool','security',1.5,3);
INSERT INTO lens_dimensions VALUES('infra-tool','maturity',1.0,4);
INSERT INTO lens_dimensions VALUES('infra-tool','complexity',1.0,5);
INSERT INTO lens_dimensions VALUES('infra-tool','stack',0.5,6);
INSERT INTO lens_dimensions VALUES('infra-tool','loc',0.5,7);
INSERT INTO lens_dimensions VALUES('content-pipeline','hooks',1.0,1);
INSERT INTO lens_dimensions VALUES('content-pipeline','loops',1.0,2);
INSERT INTO lens_dimensions VALUES('content-pipeline','memory',1.0,3);
INSERT INTO lens_dimensions VALUES('content-pipeline','security',1.0,4);
INSERT INTO lens_dimensions VALUES('content-pipeline','maturity',1.0,5);
INSERT INTO lens_dimensions VALUES('content-pipeline','complexity',1.0,6);
INSERT INTO lens_dimensions VALUES('content-pipeline','stack',0.5,7);
INSERT INTO lens_dimensions VALUES('content-pipeline','loc',0.5,8);
INSERT INTO lens_dimensions VALUES('general','hooks',1.0,1);
INSERT INTO lens_dimensions VALUES('general','loops',1.0,2);
INSERT INTO lens_dimensions VALUES('general','tool_calls',1.0,3);
INSERT INTO lens_dimensions VALUES('general','agents',1.0,4);
INSERT INTO lens_dimensions VALUES('general','skills',1.0,5);
INSERT INTO lens_dimensions VALUES('general','progressive_context',1.0,6);
INSERT INTO lens_dimensions VALUES('general','memory',1.0,7);
INSERT INTO lens_dimensions VALUES('general','agent_coordination',1.0,8);
INSERT INTO lens_dimensions VALUES('general','stack',1.0,9);
INSERT INTO lens_dimensions VALUES('general','loc',1.0,10);
INSERT INTO lens_dimensions VALUES('general','security',1.0,11);
INSERT INTO lens_dimensions VALUES('general','maturity',1.0,12);
INSERT INTO lens_dimensions VALUES('general','complexity',1.0,13);
INSERT INTO lens_dimensions VALUES('general','multi_provider',1.0,14);
INSERT INTO lens_dimensions VALUES('general','multi_modal',1.0,15);
INSERT INTO lens_dimensions VALUES('general','ui_extensibility',1.0,16);
INSERT INTO lens_dimensions VALUES('general','enterprise',1.0,17);
INSERT INTO lens_dimensions VALUES('general','data_governance',1.0,18);
