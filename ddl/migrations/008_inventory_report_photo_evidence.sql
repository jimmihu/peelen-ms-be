BEGIN;

ALTER TABLE inventory_reports
    ADD COLUMN IF NOT EXISTS evidence text;

COMMENT ON COLUMN inventory_reports.evidence IS
    'MinIO object key for optional inventory report photo evidence (JPEG, PNG, or WebP)';

COMMIT;
