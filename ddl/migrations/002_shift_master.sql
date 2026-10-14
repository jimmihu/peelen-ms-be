BEGIN;

CREATE TABLE IF NOT EXISTS shifts (
    shift      varchar(10) PRIMARY KEY CHECK (shift IN ('pagi', 'siang', 'malam')),
    start_time time NOT NULL,
    end_time   time NOT NULL
);

INSERT INTO shifts (shift, start_time, end_time) VALUES
    ('pagi', '08:00', '16:00'),
    ('siang', '16:00', '00:00'),
    ('malam', '00:00', '08:00')
ON CONFLICT (shift) DO UPDATE
SET start_time = EXCLUDED.start_time, end_time = EXCLUDED.end_time;

ALTER TABLE pipeline_reports
    DROP CONSTRAINT IF EXISTS pipeline_reports_pipeline_item_id_user_id_shift_fkey;
ALTER TABLE pipeline_item_assignments
    DROP CONSTRAINT IF EXISTS pipeline_item_assignments_shift_fkey;
ALTER TABLE pipeline_reports
    DROP CONSTRAINT IF EXISTS pipeline_reports_shift_fkey;

UPDATE pipeline_reports
SET shift = CASE lower(trim(shift))
    WHEN 'day' THEN 'pagi'
    WHEN 'daytime' THEN 'pagi'
    WHEN 'morning' THEN 'pagi'
    WHEN 'pagi' THEN 'pagi'
    WHEN 'afternoon' THEN 'siang'
    WHEN 'evening' THEN 'siang'
    WHEN 'siang' THEN 'siang'
    WHEN 'night' THEN 'malam'
    WHEN 'malam' THEN 'malam'
    ELSE lower(trim(shift))
END;

UPDATE pipeline_item_assignments
SET shift = CASE lower(trim(shift))
    WHEN 'day' THEN 'pagi'
    WHEN 'daytime' THEN 'pagi'
    WHEN 'morning' THEN 'pagi'
    WHEN 'pagi' THEN 'pagi'
    WHEN 'afternoon' THEN 'siang'
    WHEN 'evening' THEN 'siang'
    WHEN 'siang' THEN 'siang'
    WHEN 'night' THEN 'malam'
    WHEN 'malam' THEN 'malam'
    ELSE lower(trim(shift))
END;

ALTER TABLE pipeline_item_assignments
    ALTER COLUMN shift TYPE varchar(10),
    ADD CONSTRAINT pipeline_item_assignments_shift_fkey
        FOREIGN KEY (shift) REFERENCES shifts(shift) ON UPDATE CASCADE ON DELETE RESTRICT;

ALTER TABLE pipeline_reports
    ALTER COLUMN shift TYPE varchar(10),
    ADD CONSTRAINT pipeline_reports_shift_fkey
        FOREIGN KEY (shift) REFERENCES shifts(shift) ON UPDATE CASCADE ON DELETE RESTRICT;

ALTER TABLE pipeline_reports
    ADD CONSTRAINT pipeline_reports_pipeline_item_id_user_id_shift_fkey
        FOREIGN KEY (pipeline_item_id, user_id, shift)
        REFERENCES pipeline_item_assignments(pipeline_item_id, user_id, shift)
        ON DELETE RESTRICT;

COMMIT;
