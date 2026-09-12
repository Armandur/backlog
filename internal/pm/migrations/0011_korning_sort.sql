-- Sorten skiljer taskkörningar och samtalsfrågor från korta förslag.
ALTER TABLE pm_korningar ADD COLUMN sort TEXT NOT NULL DEFAULT 'task'
  CHECK(sort IN ('task','fraga','forslag'));

CREATE INDEX idx_pm_korningar_sort ON pm_korningar(sort, skapad_at);
