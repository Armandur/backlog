CREATE TABLE IF NOT EXISTS pm_anvandning (
  agent                         TEXT PRIMARY KEY,
  status                        TEXT NOT NULL DEFAULT '',
  kvottyp                       TEXT NOT NULL DEFAULT '',
  fem_timmar_andel              REAL NOT NULL,
  fem_timmar_nollstalls_at      INTEGER NOT NULL,
  sju_dagar_andel               REAL NOT NULL,
  sju_dagar_nollstalls_at       INTEGER NOT NULL,
  avlast_at                     INTEGER NOT NULL
);
