-- Körningen minns vilken modell som begärdes, vid sidan av agentens namn.
ALTER TABLE pm_korningar ADD COLUMN modell TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_korningar ADD COLUMN anstrangning TEXT NOT NULL DEFAULT '';
