-- +goose Up
-- Conventions: ids are UUIDv7 text; timestamps are unix milliseconds; no
-- booleans (0/1 integers). Positions and ids are compared bytewise: the
-- Postgres copy of this file declares a C collation on them (added by the
-- generation step in Task 9 Step 4). goose_db_version plays the role of
-- the spec's schema_version table.
CREATE TABLE workspace (
    id TEXT PRIMARY KEY COLLATE "C",
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    next_seq BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE TABLE app_user (
    id TEXT PRIMARY KEY COLLATE "C",
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    email TEXT,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'human',
    password_hash TEXT,
    email_verified_at BIGINT,
    locale TEXT NOT NULL DEFAULT 'en',
    disabled_at BIGINT,
    deleted_at BIGINT,
    anonymized_at BIGINT,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE (workspace_id, email)
);

CREATE TABLE workspace_member (
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    user_id TEXT NOT NULL REFERENCES app_user(id),
    role TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);

CREATE TABLE project (
    id TEXT PRIMARY KEY COLLATE "C",
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    key TEXT NOT NULL,
    name TEXT NOT NULL,
    next_card_number BIGINT NOT NULL DEFAULT 0,
    estimate_unit TEXT,
    version BIGINT NOT NULL DEFAULT 1,
    archived_at BIGINT,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE (workspace_id, key)
);

CREATE TABLE project_member (
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    project_id TEXT NOT NULL REFERENCES project(id),
    user_id TEXT NOT NULL REFERENCES app_user(id),
    role TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (project_id, user_id)
);

CREATE TABLE project_key_history (
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    project_id TEXT NOT NULL REFERENCES project(id),
    old_key TEXT NOT NULL,
    changed_at BIGINT NOT NULL,
    PRIMARY KEY (project_id, old_key)
);

CREATE TABLE board (
    id TEXT PRIMARY KEY COLLATE "C",
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    project_id TEXT NOT NULL REFERENCES project(id),
    name TEXT NOT NULL,
    position TEXT NOT NULL COLLATE "C",
    version BIGINT NOT NULL DEFAULT 1,
    archived_at BIGINT,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE (project_id, position)
);

CREATE TABLE board_column (
    id TEXT PRIMARY KEY COLLATE "C",
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    board_id TEXT NOT NULL REFERENCES board(id),
    name TEXT NOT NULL,
    position TEXT NOT NULL COLLATE "C",
    category TEXT NOT NULL,
    wip_limit BIGINT,
    version BIGINT NOT NULL DEFAULT 1,
    archived_at BIGINT,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE (board_id, position)
);

CREATE TABLE card (
    id TEXT PRIMARY KEY COLLATE "C",
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    project_id TEXT NOT NULL REFERENCES project(id),
    board_id TEXT NOT NULL REFERENCES board(id),
    column_id TEXT NOT NULL REFERENCES board_column(id),
    number BIGINT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position TEXT NOT NULL COLLATE "C",
    due_date BIGINT,
    created_by TEXT NOT NULL REFERENCES app_user(id),
    completed_at BIGINT,
    archived_at BIGINT,
    version BIGINT NOT NULL DEFAULT 1,
    type_id TEXT,
    parent_id TEXT REFERENCES card(id),
    priority BIGINT,
    estimate DOUBLE PRECISION,
    start_date BIGINT,
    resolution TEXT,
    iteration_id TEXT,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE (project_id, number),
    UNIQUE (column_id, position)
);
CREATE INDEX card_board ON card (board_id, archived_at);
CREATE INDEX card_updated ON card (project_id, updated_at);

CREATE TABLE label (
    id TEXT PRIMARY KEY COLLATE "C",
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    project_id TEXT NOT NULL REFERENCES project(id),
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'label',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE card_label (
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    card_id TEXT NOT NULL REFERENCES card(id),
    label_id TEXT NOT NULL REFERENCES label(id),
    PRIMARY KEY (card_id, label_id)
);

CREATE TABLE card_assignee (
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    card_id TEXT NOT NULL REFERENCES card(id),
    user_id TEXT NOT NULL REFERENCES app_user(id),
    PRIMARY KEY (card_id, user_id)
);

CREATE TABLE comment (
    id TEXT PRIMARY KEY COLLATE "C",
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    card_id TEXT NOT NULL REFERENCES card(id),
    author_id TEXT NOT NULL REFERENCES app_user(id),
    body TEXT NOT NULL,
    via_token_id TEXT,
    edited_at BIGINT,
    deleted_at BIGINT,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
CREATE INDEX comment_card ON comment (card_id, created_at);

CREATE TABLE event (
    id TEXT PRIMARY KEY COLLATE "C",
    workspace_id TEXT NOT NULL REFERENCES workspace(id),
    project_id TEXT,
    board_id TEXT,
    card_id TEXT,
    seq BIGINT NOT NULL,
    actor_user_id TEXT,
    via_token_id TEXT,
    actor_kind TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload TEXT NOT NULL,
    occurred_at BIGINT NOT NULL,
    UNIQUE (workspace_id, seq)
);
CREATE INDEX event_card ON event (card_id, occurred_at);
CREATE INDEX event_project ON event (project_id, occurred_at);

CREATE TABLE job (
    id TEXT PRIMARY KEY COLLATE "C",
    workspace_id TEXT,
    kind TEXT NOT NULL,
    payload TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'queued',
    attempts BIGINT NOT NULL DEFAULT 0,
    max_attempts BIGINT NOT NULL DEFAULT 8,
    run_at BIGINT NOT NULL,
    lease_owner TEXT,
    lease_expires_at BIGINT,
    last_error TEXT,
    created_at BIGINT NOT NULL,
    completed_at BIGINT
);
CREATE INDEX job_due ON job (state, run_at);

CREATE TABLE command_receipt (
    workspace_id TEXT,
    idempotency_key TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    result TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (idempotency_key, actor_id)
);
CREATE INDEX command_receipt_created ON command_receipt (created_at);
