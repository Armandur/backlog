CREATE TABLE IF NOT EXISTS pm_portar (
  port           INTEGER PRIMARY KEY,
  projekt        TEXT NOT NULL,
  pid            INTEGER NOT NULL,
  reserverad_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pm_portar_reserverad_at ON pm_portar(reserverad_at);
