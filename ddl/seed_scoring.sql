-- =====================================================
-- Contoh aturan SLA & maturity (development/testing).
-- Aman dijalankan berulang dan tanpa reset: hanya menambah aturan yang belum ada.
-- Jalankan setelah seed_v3.sql, lalu isi fakta & skor dengan:
--   make scoring-backfill   (atau jalankan server; job-nya mengisi otomatis)
--
--   SLA      : default per vendor, bulanan (COMPLETION 40% >=95, ON_TIME 40% >=90,
--              EVIDENCE 20% >=98) + override lebih ketat untuk PT Mitra di PLTU Paiton
--   MATURITY : default per vendor, bulanan, level default (gate 2-5)
-- Aturan dibuat "120 hari lalu" supaya periode lampau ikut dinilai.
-- =====================================================

BEGIN;

-- SLA default per vendor
INSERT INTO score_rules (kind, vendor_id, site_id, period_type, version, created_by, created_at)
SELECT 'SLA', v.id, NULL, 'MONTH', 1, sa.id, now() - interval '120 days'
FROM vendors v
CROSS JOIN (SELECT id FROM users WHERE username = 'superadmin') sa
WHERE NOT EXISTS (
    SELECT 1 FROM score_rules r WHERE r.kind = 'SLA' AND r.vendor_id = v.id AND r.site_id IS NULL
);

-- SLA override PT Mitra di PLTU Paiton (dibuat admin.paiton)
INSERT INTO score_rules (kind, vendor_id, site_id, period_type, version, created_by, created_at)
SELECT 'SLA', v.id, s.id, 'MONTH', 1, ad.id, now() - interval '120 days'
FROM vendors v
JOIN sites s ON s.code = 'PAITON'
JOIN vendor_sites vs ON vs.vendor_id = v.id AND vs.site_id = s.id
LEFT JOIN users ad ON ad.username = 'admin.paiton'
WHERE v.code = 'VND-001'
  AND NOT EXISTS (
      SELECT 1 FROM score_rules r WHERE r.kind = 'SLA' AND r.vendor_id = v.id AND r.site_id = s.id
  );

INSERT INTO score_rule_items (rule_id, code, weight, target)
SELECT r.id, x.code, x.weight, CASE WHEN r.site_id IS NULL THEN x.target ELSE x.strict END
FROM score_rules r
CROSS JOIN (VALUES
    ('COMPLETION_RATE', 40, 95, 97),
    ('ON_TIME_RATE', 40, 90, 95),
    ('EVIDENCE_COMPLETE_RATE', 20, 98, 99)
) AS x(code, weight, target, strict)
WHERE r.kind = 'SLA'
  AND NOT EXISTS (SELECT 1 FROM score_rule_items i WHERE i.rule_id = r.id);

-- Maturity default per vendor
INSERT INTO score_rules (kind, vendor_id, site_id, period_type, version, created_by, created_at)
SELECT 'MATURITY', v.id, NULL, 'MONTH', 1, sa.id, now() - interval '120 days'
FROM vendors v
CROSS JOIN (SELECT id FROM users WHERE username = 'superadmin') sa
WHERE NOT EXISTS (
    SELECT 1 FROM score_rules r WHERE r.kind = 'MATURITY' AND r.vendor_id = v.id AND r.site_id IS NULL
);

INSERT INTO score_rule_items (rule_id, code, weight)
SELECT r.id, x.code, x.weight
FROM score_rules r
CROSS JOIN (VALUES ('COMPLETION_RATE', 30), ('ON_TIME_RATE', 30), ('EVIDENCE_COMPLETE_RATE', 15),
    ('LATE_UPLOAD_RATE', 10), ('CONSISTENCY', 15)) AS x(code, weight)
WHERE r.kind = 'MATURITY'
  AND NOT EXISTS (SELECT 1 FROM score_rule_items i WHERE i.rule_id = r.id);

INSERT INTO score_rule_levels (rule_id, level, min_score, min_dimension, consecutive_periods)
SELECT r.id, l.level, l.min_score, l.min_dimension, l.periods
FROM score_rules r
CROSS JOIN (VALUES (2, 60, 50, 1), (3, 75, 65, 2), (4, 85, 75, 3), (5, 95, 85, 3))
    AS l(level, min_score, min_dimension, periods)
WHERE r.kind = 'MATURITY'
  AND NOT EXISTS (SELECT 1 FROM score_rule_levels x WHERE x.rule_id = r.id);

-- Ringkasan
SELECT r.kind, v.code AS vendor, s.code AS site, r.period_type,
       (SELECT count(*) FROM score_rule_items i WHERE i.rule_id = r.id) AS items,
       (SELECT count(*) FROM score_rule_levels l WHERE l.rule_id = r.id) AS levels
FROM score_rules r
JOIN vendors v ON v.id = r.vendor_id
LEFT JOIN sites s ON s.id = r.site_id
WHERE r.superseded_at IS NULL
ORDER BY r.kind, v.code, s.code NULLS FIRST;

COMMIT;
