CREATE TABLE IF NOT EXISTS download_tasks (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL DEFAULT '',
    source_url TEXT NOT NULL,
    video_id TEXT NOT NULL DEFAULT '',
    progress DOUBLE PRECISION NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_download_tasks_created_at ON download_tasks(created_at DESC);
