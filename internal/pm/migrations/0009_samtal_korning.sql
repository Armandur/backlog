-- Ett inlägg som agenten svarat på minns vilken körning som skrev svaret.
-- Utan kopplingen försvinner agentens arbete när sidan laddas om.
ALTER TABLE pm_samtal ADD COLUMN korning_id TEXT;
