BEGIN;

-- Each site keeps its own IANA time zone (Asia/Jakarta = WIB, Asia/Makassar = WITA,
-- Asia/Jayapura = WIT). Shift windows, "today", upload windows, and photo times
-- without an offset are evaluated in the zone of the site where the task is.
ALTER TABLE sites ADD COLUMN IF NOT EXISTS timezone varchar(64) NOT NULL DEFAULT 'Asia/Jakarta';

COMMIT;
