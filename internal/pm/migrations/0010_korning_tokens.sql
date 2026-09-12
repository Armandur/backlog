-- Körningen minns hur många tokens den kostade, så vyn slipper läsa
-- händelsefilen bara för att visa siffran.
ALTER TABLE pm_korningar ADD COLUMN tokens INTEGER NOT NULL DEFAULT 0;
