BEGIN;

-- Pipeline reports no longer go through manager review.
--   status        : IN_PROGRESS | COMPLETED
--   status_revisi : NONE | SUBMITTED (late upload after the shift ended)
ALTER TABLE pipeline_reports
    DROP CONSTRAINT IF EXISTS pipeline_reports_status_check,
    DROP CONSTRAINT IF EXISTS pipeline_reports_status_revisi_check;
ALTER TABLE pipeline_report_history
    DROP CONSTRAINT IF EXISTS pipeline_report_history_new_status_check,
    DROP CONSTRAINT IF EXISTS pipeline_report_history_new_revision_check;

-- Under the old flow SUBMITTED meant the Worker had finished and was waiting for
-- review, so it maps to COMPLETED when the report has its closing evidence.
-- Everything else that was not COMPLETED is still open work.
UPDATE pipeline_reports pr
SET status = CASE
        WHEN pr.status = 'COMPLETED' THEN 'COMPLETED'
        WHEN pr.status = 'SUBMITTED' AND EXISTS (
            SELECT 1 FROM pipeline_report_details d
            WHERE d.pipeline_report_id = pr.id
              AND d.evidence_after IS NOT NULL AND d.closed_date IS NOT NULL
        ) THEN 'COMPLETED'
        ELSE 'IN_PROGRESS'
    END,
    status_revisi = CASE WHEN pr.status_revisi = 'RESUBMITTED' THEN 'SUBMITTED' ELSE 'NONE' END;

UPDATE pipeline_report_history
SET previous_status = CASE
        WHEN previous_status IS NULL THEN NULL
        WHEN previous_status IN ('COMPLETED', 'SUBMITTED') THEN 'COMPLETED'
        ELSE 'IN_PROGRESS'
    END,
    new_status = CASE WHEN new_status IN ('COMPLETED', 'SUBMITTED') THEN 'COMPLETED' ELSE 'IN_PROGRESS' END,
    previous_revision = CASE
        WHEN previous_revision IS NULL THEN NULL
        WHEN previous_revision = 'RESUBMITTED' THEN 'SUBMITTED'
        ELSE 'NONE'
    END,
    new_revision = CASE WHEN new_revision = 'RESUBMITTED' THEN 'SUBMITTED' ELSE 'NONE' END;

ALTER TABLE pipeline_reports
    ALTER COLUMN status SET DEFAULT 'IN_PROGRESS',
    ADD CONSTRAINT pipeline_reports_status_check
        CHECK (status IN ('IN_PROGRESS', 'COMPLETED')),
    ADD CONSTRAINT pipeline_reports_status_revisi_check
        CHECK (status_revisi IN ('NONE', 'SUBMITTED'));
ALTER TABLE pipeline_report_history
    ADD CONSTRAINT pipeline_report_history_new_status_check
        CHECK (new_status IN ('IN_PROGRESS', 'COMPLETED')),
    ADD CONSTRAINT pipeline_report_history_new_revision_check
        CHECK (new_revision IN ('NONE', 'SUBMITTED'));

COMMIT;
