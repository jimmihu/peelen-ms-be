-- Operational development fixtures for PRD 1.2.
-- Run after init.sql and seed.sql. Safe to re-run on the same seeded database.

BEGIN;

INSERT INTO inventories (vendor_id, item, jenis_item, qty, status_condition, created_by)
SELECT v.id, fixture.item, fixture.item_type, fixture.qty, fixture.condition, u.id
FROM (VALUES
    ('VND-001', 'Safety helmet', 'PPE', 25, 'GOOD'),
    ('VND-001', 'Safety vest', 'PPE', 18, 'GOOD'),
    ('VND-002', 'Fire extinguisher', 'Safety equipment', 8, 'MAINTENANCE')
) AS fixture(vendor_code, item, item_type, qty, condition)
JOIN vendors v ON v.code = fixture.vendor_code
CROSS JOIN users u
WHERE u.username = 'superadmin'
  AND NOT EXISTS (
      SELECT 1 FROM inventories existing
      WHERE existing.vendor_id = v.id AND existing.item = fixture.item
  );

INSERT INTO items (name, description, created_by)
SELECT fixture.name, fixture.description, u.id
FROM (VALUES
    ('Inspect PPE', 'Check condition and availability of assigned personal protective equipment'),
    ('Inspect fire extinguisher', 'Verify pressure gauge, seal, and inspection date'),
    ('Inspect work area', 'Check housekeeping and identify visible hazards')
) AS fixture(name, description)
CROSS JOIN users u
WHERE u.username = 'superadmin'
  AND NOT EXISTS (
      SELECT 1 FROM items existing WHERE existing.name = fixture.name
  );

INSERT INTO pipelines (vendor_id, name, description, created_by)
SELECT v.id, 'Daily Safety Inspection',
       'Example daily safety checklist for development and API testing', u.id
FROM vendors v
CROSS JOIN users u
WHERE v.code = 'VND-001'
  AND u.username = 'superadmin'
  AND NOT EXISTS (
      SELECT 1 FROM pipelines existing
      WHERE existing.vendor_id = v.id AND existing.name = 'Daily Safety Inspection'
  );

INSERT INTO pipeline_items (pipeline_id, item_id, sort_order, created_by)
SELECT p.id, i.id, fixture.sort_order, u.id
FROM (VALUES
    ('Inspect PPE', 1),
    ('Inspect work area', 2)
) AS fixture(item_name, sort_order)
JOIN pipelines p ON p.name = 'Daily Safety Inspection'
JOIN vendors v ON v.id = p.vendor_id AND v.code = 'VND-001'
JOIN items i ON i.name = fixture.item_name
CROSS JOIN users u
WHERE u.username = 'superadmin'
ON CONFLICT (pipeline_id, item_id) DO NOTHING;

INSERT INTO pipeline_item_assignments (pipeline_item_id, user_id, shift, assigned_by)
SELECT pi.id, worker.id, 'DAY', admin.id
FROM pipeline_items pi
JOIN pipelines p ON p.id = pi.pipeline_id AND p.name = 'Daily Safety Inspection'
JOIN vendors v ON v.id = p.vendor_id AND v.code = 'VND-001'
JOIN items i ON i.id = pi.item_id
JOIN users worker ON worker.username = 'worker01'
CROSS JOIN users admin
WHERE admin.username = 'superadmin'
  AND i.name IN ('Inspect PPE', 'Inspect work area')
ON CONFLICT (pipeline_item_id, user_id, shift) DO NOTHING;

COMMIT;
