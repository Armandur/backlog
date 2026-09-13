-- Codex har inte alltid båda tidsfönstren. Kontots codex-kvot bär bara sju
-- dagar, medan modellkvoterna bär både fem timmar och sju dagar. Kolumnerna
-- blir därför nullbara, så PM kan spara det fönster som faktiskt finns i
-- stället för att hitta på en nolla.
CREATE TABLE pm_anvandning_ny (
  agent                         TEXT PRIMARY KEY,
  status                        TEXT NOT NULL DEFAULT '',
  kvottyp                       TEXT NOT NULL DEFAULT '',
  fem_timmar_andel              REAL,
  fem_timmar_nollstalls_at      INTEGER,
  sju_dagar_andel               REAL,
  sju_dagar_nollstalls_at       INTEGER,
  avlast_at                     INTEGER NOT NULL
);

INSERT INTO pm_anvandning_ny(agent, status, kvottyp, fem_timmar_andel,
  fem_timmar_nollstalls_at, sju_dagar_andel, sju_dagar_nollstalls_at, avlast_at)
SELECT agent, status, kvottyp, fem_timmar_andel, fem_timmar_nollstalls_at,
  sju_dagar_andel, sju_dagar_nollstalls_at, avlast_at
FROM pm_anvandning;

DROP TABLE pm_anvandning;
ALTER TABLE pm_anvandning_ny RENAME TO pm_anvandning;
