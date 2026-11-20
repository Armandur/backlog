-- A row means task_id cannot start until depends_on_id is done.
CREATE TABLE IF NOT EXISTS task_dependencies (
  task_id        TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  depends_on_id  TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at     INTEGER NOT NULL,
  PRIMARY KEY(task_id, depends_on_id),
  CHECK(task_id <> depends_on_id)
);

CREATE INDEX IF NOT EXISTS idx_task_dependencies_depends_on ON task_dependencies(depends_on_id);
