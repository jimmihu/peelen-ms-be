BEGIN;

-- Rename shift codes pagi/siang/malam -> DAY/EVENING/NIGHT.
-- pipeline_reports.shift follows via ON UPDATE CASCADE.
ALTER TABLE shifts DROP CONSTRAINT IF EXISTS shifts_shift_check;

UPDATE shifts
SET shift = CASE shift
    WHEN 'pagi'  THEN 'DAY'
    WHEN 'siang' THEN 'EVENING'
    WHEN 'malam' THEN 'NIGHT'
    ELSE shift
END;

ALTER TABLE shifts
    ADD CONSTRAINT shifts_shift_check CHECK (shift IN ('DAY', 'EVENING', 'NIGHT'));

COMMIT;
