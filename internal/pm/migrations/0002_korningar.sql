CREATE TABLE IF NOT EXISTS pm_korningar (
  id           TEXT PRIMARY KEY,
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id      TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  task_ref     TEXT NOT NULL DEFAULT '',
  agent        TEXT NOT NULL,
  motivering   TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'koad',
  repo_path    TEXT NOT NULL DEFAULT '',
  pid          INTEGER NOT NULL DEFAULT 0,
  exit_kod     INTEGER,
  logg_sokvag  TEXT NOT NULL DEFAULT '',
  skapad_at    INTEGER NOT NULL,
  startad_at   INTEGER,
  slut_at      INTEGER,
  CHECK(status IN ('koad','kor','klar','fel'))
);

CREATE INDEX IF NOT EXISTS idx_pm_korningar_task ON pm_korningar(task_id, skapad_at);
CREATE INDEX IF NOT EXISTS idx_pm_korningar_status ON pm_korningar(status, repo_path);
