CREATE TABLE IF NOT EXISTS pm_testservrar (
  alias        TEXT PRIMARY KEY,
  pid          INTEGER NOT NULL,
  port         INTEGER NOT NULL,
  startad_at   INTEGER NOT NULL,
  logg_sokvag TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pm_testservrar_pid ON pm_testservrar(pid);
