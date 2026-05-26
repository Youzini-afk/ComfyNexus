-- 002_jobs.sql -- ensure Phase 2 jobs schema/indexes exist on upgraded DBs
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
CREATE INDEX IF NOT EXISTS idx_jobs_type_created ON jobs(type, created_at);
