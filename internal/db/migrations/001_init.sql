-- 001_init.sql -- ComfyNexus initial schema
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    username        TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    totp_secret     TEXT NOT NULL,
    role            TEXT NOT NULL DEFAULT 'admin',
    locale          TEXT NOT NULL DEFAULT 'zh-CN',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at   DATETIME
);

CREATE TABLE IF NOT EXISTS sessions (
    token       TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME NOT NULL,
    last_seen   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ip          TEXT,
    user_agent  TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS login_attempts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ip          TEXT NOT NULL,
    username    TEXT,
    success     INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_login_attempts_ip_time ON login_attempts(ip, created_at);

CREATE TABLE IF NOT EXISTS gpu_instances (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT NOT NULL,
    ssh_host            TEXT NOT NULL,
    ssh_port            INTEGER NOT NULL DEFAULT 22,
    ssh_user            TEXT NOT NULL,
    ssh_key_source      TEXT NOT NULL,            -- 'inline' | 'mounted'
    ssh_key_blob        BLOB,                     -- AES-GCM encrypted PEM (inline)
    ssh_key_path        TEXT,                     -- file path (mounted)
    ssh_passphrase_blob BLOB,                     -- AES-GCM encrypted passphrase
    ssh_host_fingerprint TEXT,                    -- optional pinned host fingerprint
    comfy_host          TEXT NOT NULL DEFAULT '127.0.0.1',
    comfy_port          INTEGER NOT NULL DEFAULT 8188,
    comfy_root          TEXT,                     -- ComfyUI install root, e.g. /workspace/ComfyUI
    comfy_start_cmd     TEXT,                     -- start/restart shell command
    notes               TEXT,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Models registry (populated in Phase 3)
CREATE TABLE IF NOT EXISTS models (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id         INTEGER NOT NULL REFERENCES gpu_instances(id) ON DELETE CASCADE,
    model_type          TEXT NOT NULL,
    filename            TEXT NOT NULL,
    rel_path            TEXT NOT NULL,
    size_bytes          INTEGER NOT NULL DEFAULT 0,
    sha256              TEXT,
    civitai_model_id    INTEGER,
    civitai_version_id  INTEGER,
    hf_repo             TEXT,
    hf_file             TEXT,
    trigger_words       TEXT,
    base_model          TEXT,
    metadata_json       TEXT,
    thumbnail_path      TEXT,
    tags                TEXT,
    scanned_at          DATETIME,
    civitai_synced_at   DATETIME,
    UNIQUE(instance_id, rel_path)
);
CREATE INDEX IF NOT EXISTS idx_models_sha256 ON models(sha256);
CREATE INDEX IF NOT EXISTS idx_models_type ON models(model_type);

-- Images registry (populated in Phase 3)
CREATE TABLE IF NOT EXISTS images (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id     INTEGER NOT NULL REFERENCES gpu_instances(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL,
    rel_path        TEXT NOT NULL,
    mtime           DATETIME NOT NULL,
    size_bytes      INTEGER NOT NULL DEFAULT 0,
    width           INTEGER,
    height          INTEGER,
    prompt          TEXT,
    negative_prompt TEXT,
    workflow_json   TEXT,
    params_json     TEXT,
    used_models     TEXT,
    thumbnail_path  TEXT,
    favorited       INTEGER NOT NULL DEFAULT 0,
    tags            TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(instance_id, rel_path)
);
CREATE INDEX IF NOT EXISTS idx_images_instance_mtime ON images(instance_id, mtime DESC);
CREATE INDEX IF NOT EXISTS idx_images_favorited ON images(favorited);

-- Background jobs (download, scan, sync)
CREATE TABLE IF NOT EXISTS jobs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    type            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    payload_json    TEXT NOT NULL DEFAULT '{}',
    progress        INTEGER NOT NULL DEFAULT 0,
    total           INTEGER NOT NULL DEFAULT 0,
    message         TEXT,
    error           TEXT,
    instance_id     INTEGER REFERENCES gpu_instances(id) ON DELETE SET NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at      DATETIME,
    finished_at     DATETIME
);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status, created_at);
