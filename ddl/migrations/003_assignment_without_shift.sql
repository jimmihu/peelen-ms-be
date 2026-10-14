BEGIN;

-- Assignments are now per (pipeline item, user); shift moves to the report only.
-- Pick one assignment per (pipeline item, user) to keep: active first, then oldest.
CREATE TEMP TABLE assignment_merge ON COMMIT DROP AS
SELECT a.id AS old_id,
       first_value(a.id) OVER (
           PARTITION BY a.pipeline_item_id, a.user_id
           ORDER BY a.is_active DESC, a.created_at, a.id
       ) AS keep_id
FROM pipeline_item_assignments a;

ALTER TABLE pipeline_reports
    DROP CONSTRAINT IF EXISTS pipeline_reports_pipeline_item_id_user_id_shift_fkey,
    DROP CONSTRAINT IF EXISTS pipeline_reports_pipeline_item_id_user_id_shift_report_date_key,
    DROP CONSTRAINT IF EXISTS pipeline_reports_id_user_id_key,
    ADD COLUMN IF NOT EXISTS pipeline_item_assignment_id uuid;

UPDATE pipeline_reports pr
SET pipeline_item_assignment_id = m.keep_id
FROM pipeline_item_assignments a
JOIN assignment_merge m ON m.old_id = a.id
WHERE a.pipeline_item_id = pr.pipeline_item_id
  AND a.user_id = pr.user_id
  AND a.shift = pr.shift;

DELETE FROM pipeline_item_assignments a
USING assignment_merge m
WHERE m.old_id = a.id AND m.old_id <> m.keep_id;

ALTER TABLE pipeline_item_assignments
    DROP CONSTRAINT IF EXISTS pipeline_item_assignments_pipeline_item_id_user_id_shift_key,
    DROP CONSTRAINT IF EXISTS pipeline_item_assignments_shift_fkey,
    DROP COLUMN IF EXISTS shift,
    ADD CONSTRAINT pipeline_item_assignments_pipeline_item_id_user_id_key
        UNIQUE (pipeline_item_id, user_id);

DROP INDEX IF EXISTS idx_pipeline_reports_user_date;
DROP INDEX IF EXISTS idx_pipeline_reports_item_date;

ALTER TABLE pipeline_reports
    ALTER COLUMN pipeline_item_assignment_id SET NOT NULL,
    ADD CONSTRAINT pipeline_reports_pipeline_item_assignment_id_fkey
        FOREIGN KEY (pipeline_item_assignment_id)
        REFERENCES pipeline_item_assignments(id) ON DELETE RESTRICT,
    DROP COLUMN IF EXISTS pipeline_item_id,
    DROP COLUMN IF EXISTS user_id,
    ADD CONSTRAINT pipeline_reports_pipeline_item_assignment_id_shift_report_date_key
        UNIQUE (pipeline_item_assignment_id, shift, report_date);

CREATE INDEX IF NOT EXISTS idx_pipeline_reports_assignment_date
    ON pipeline_reports(pipeline_item_assignment_id, report_date DESC);

COMMIT;
