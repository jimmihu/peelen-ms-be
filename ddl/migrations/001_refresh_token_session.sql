BEGIN;

ALTER TABLE refresh_tokens
    ADD COLUMN IF NOT EXISTS session_id uuid REFERENCES sessions(id) ON DELETE CASCADE;

UPDATE refresh_tokens
SET revoked_at = COALESCE(revoked_at, now())
WHERE session_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_audit_logs_user_created
    ON audit_logs(user_id, created_at DESC);

COMMIT;
