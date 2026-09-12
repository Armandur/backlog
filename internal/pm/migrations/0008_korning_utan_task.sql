-- En fråga i projektsamtalet blir också en körning, men den hör inte till
-- någon task. Därför byggs tabellen om med task_id som får vara tom.
-- SQLite kan inte lätta ett NOT NULL i efterhand, så tabellen skapas om.
CREATE TABLE pm_korningar_ny (
  id           TEXT PRIMARY KEY,
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id      TEXT REFERENCES tasks(id) ON DELETE CASCADE,
  task_ref     TEXT NOT NULL DEFAULT '',
  agent        TEXT NOT NULL,
  motivering   TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'koad',
  repo_path    TEXT NOT NULL DEFAULT '',
  pid          INTEGER NOT NULL DEFAULT 0,
  exit_kod     INTEGER,
  logg_sokvag  TEXT NOT NULL DEFAULT '',
  modell       TEXT NOT NULL DEFAULT '',
  anstrangning TEXT NOT NULL DEFAULT '',
  skapad_at    INTEGER NOT NULL,
  startad_at   INTEGER,
  slut_at      INTEGER,
  CHECK(status IN ('koad','kor','klar','fel'))
);

INSERT INTO pm_korningar_ny
  SELECT id, project_id, task_id, task_ref, agent, motivering, status, repo_path,
         pid, exit_kod, logg_sokvag, modell, anstrangning, skapad_at, startad_at, slut_at
  FROM pm_korningar;

DROP TABLE pm_korningar;
ALTER TABLE pm_korningar_ny RENAME TO pm_korningar;

CREATE INDEX IF NOT EXISTS idx_pm_korningar_task ON pm_korningar(task_id, skapad_at);
CREATE INDEX IF NOT EXISTS idx_pm_korningar_status ON pm_korningar(status, repo_path);
