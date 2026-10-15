BEGIN;

-- Each pipeline is a work area of one vendor at one site, so scores can be
-- grouped per site.
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS site_id uuid REFERENCES sites(id) ON DELETE RESTRICT;

-- Vendors with exactly one site: assign it automatically.
UPDATE pipelines p
SET site_id = vs.site_id
FROM (
    SELECT vendor_id, min(site_id::text)::uuid AS site_id
    FROM vendor_sites GROUP BY vendor_id HAVING count(*) = 1
) vs
WHERE p.vendor_id = vs.vendor_id AND p.site_id IS NULL;

DO $$
DECLARE missing text;
BEGIN
    SELECT string_agg(id::text || ' (' || name || ')', ', ') INTO missing
    FROM pipelines WHERE site_id IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'Set pipelines.site_id manually before running this migration: %', missing;
    END IF;
END $$;

ALTER TABLE pipelines ALTER COLUMN site_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_pipelines_site_id ON pipelines(site_id);

COMMIT;
