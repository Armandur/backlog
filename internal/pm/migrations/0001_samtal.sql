CREATE TABLE IF NOT EXISTS pm_samtal (
  id          TEXT PRIMARY KEY,
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id     TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  actor_kind  TEXT NOT NULL DEFAULT 'human',
  actor_name  TEXT NOT NULL DEFAULT '',
  text        TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  CHECK(actor_kind IN ('human','ai'))
);

CREATE INDEX IF NOT EXISTS idx_pm_samtal_project ON pm_samtal(project_id, created_at);
