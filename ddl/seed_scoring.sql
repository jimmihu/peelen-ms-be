-- =====================================================
-- Aturan SLA & maturity untuk development dan E2E testing.
-- Aman dijalankan berulang: hanya menambah aturan yang belum ada.
-- Jalankan setelah seed_v3.sql (make db-reset menjalankan keduanya), lalu isi
-- fakta & skor dengan `make scoring-backfill` (atau jalankan server).
--
-- Jenis periode sengaja dibuat berbeda supaya WEEK, MONTH, dan QUARTER muncul di
-- ketiga cakupan (VENDOR, SITE, WORKER). Worker 01-05 bekerja di site pertama
-- vendor (urut kode site), worker 06-10 di site kedua.
--
--   Vendor              Default (VENDOR + site tanpa override)   Override site
--   VND-001 Mitra       MONTH   standar                          PAITON    WEEK    ketat
--   VND-002 Karya       WEEK    standar                          SURALAYA  QUARTER ketat
--   VND-003 Sinergi     QUARTER ketepatan (pakai indikator       REMBANG   MONTH   ketat
--                               "makin rendah makin baik")
--   VND-004 Bumi        MONTH   standar                          -
--   VND-005 Cahaya      WEEK    dengan temuan K3                 ADIPALA   QUARTER ketat
--
-- Maturity: satu aturan default per vendor dengan periode yang sama seperti SLA
-- default vendor tsb, dimensi dan level default.
-- Semua aturan bertanggal awal kuartal lalu (sama dengan awal data laporan), sehingga
-- setiap periode yang sudah selesai sejak itu ikut dinilai dan tidak ada periode kosong
-- sebelum data dimulai.
-- =====================================================

BEGIN;

CREATE TEMP TABLE rule_plan (
    kind        text,
    vendor_code text,
    site_code   text,   -- NULL = default vendor
    period_type text,
    item_set    text,
    created_by  text
) ON COMMIT DROP;

INSERT INTO rule_plan VALUES
    ('SLA',      'VND-001', NULL,       'MONTH',   'standard',   'superadmin'),
    ('SLA',      'VND-001', 'PAITON',   'WEEK',    'strict',     'admin.paiton'),
    ('SLA',      'VND-002', NULL,       'WEEK',    'standard',   'superadmin'),
    ('SLA',      'VND-002', 'SURALAYA', 'QUARTER', 'strict',     'admin.suralaya'),
    ('SLA',      'VND-003', NULL,       'QUARTER', 'timeliness', 'superadmin'),
    ('SLA',      'VND-003', 'REMBANG',  'MONTH',   'strict',     'admin.rembang'),
    ('SLA',      'VND-004', NULL,       'MONTH',   'standard',   'superadmin'),
    ('SLA',      'VND-005', NULL,       'WEEK',    'k3',         'superadmin'),
    ('SLA',      'VND-005', 'ADIPALA',  'QUARTER', 'strict',     'admin.adipala'),
    ('MATURITY', 'VND-001', NULL,       'MONTH',   'maturity',   'superadmin'),
    ('MATURITY', 'VND-002', NULL,       'WEEK',    'maturity',   'superadmin'),
    ('MATURITY', 'VND-003', NULL,       'QUARTER', 'maturity',   'superadmin'),
    ('MATURITY', 'VND-004', NULL,       'MONTH',   'maturity',   'superadmin'),
    ('MATURITY', 'VND-005', NULL,       'WEEK',    'maturity',   'superadmin');

CREATE TEMP TABLE item_plan (item_set text, code text, weight numeric, target numeric) ON COMMIT DROP;

INSERT INTO item_plan VALUES
    ('standard',   'COMPLETION_RATE',        40, 95),
    ('standard',   'ON_TIME_RATE',           40, 90),
    ('standard',   'EVIDENCE_COMPLETE_RATE', 20, 98),
    ('strict',     'COMPLETION_RATE',        40, 97),
    ('strict',     'ON_TIME_RATE',           40, 95),
    ('strict',     'EVIDENCE_COMPLETE_RATE', 20, 99),
    ('timeliness', 'COMPLETION_RATE',        30, 95),
    ('timeliness', 'ON_TIME_RATE',           30, 90),
    ('timeliness', 'AVG_LATE_MINUTES',       20, 15),
    ('timeliness', 'LATE_UPLOAD_RATE',       20, 10),
    ('k3',         'COMPLETION_RATE',        35, 95),
    ('k3',         'ON_TIME_RATE',           35, 90),
    ('k3',         'EVIDENCE_COMPLETE_RATE', 15, 98),
    ('k3',         'K3_FINDING_NOTES_RATE',  15, 100),
    ('maturity',   'COMPLETION_RATE',        30, NULL),
    ('maturity',   'ON_TIME_RATE',           30, NULL),
    ('maturity',   'EVIDENCE_COMPLETE_RATE', 15, NULL),
    ('maturity',   'LATE_UPLOAD_RATE',       10, NULL),
    ('maturity',   'CONSISTENCY',            15, NULL);

-- Aturan yang belum ada
INSERT INTO score_rules (kind, vendor_id, site_id, period_type, version, created_by, created_at)
SELECT rp.kind, v.id, s.id, rp.period_type, 1, u.id,
       (date_trunc('quarter', current_date) - interval '3 months')
FROM rule_plan rp
JOIN vendors v ON v.code = rp.vendor_code
LEFT JOIN sites s ON s.code = rp.site_code
LEFT JOIN users u ON u.username = rp.created_by
WHERE (rp.site_code IS NULL OR EXISTS (
          SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = v.id AND vs.site_id = s.id))
  AND NOT EXISTS (
      SELECT 1 FROM score_rules r
      WHERE r.kind = rp.kind AND r.vendor_id = v.id
        AND r.site_id IS NOT DISTINCT FROM s.id
  );

-- Item untuk aturan yang belum punya item
INSERT INTO score_rule_items (rule_id, code, weight, target)
SELECT r.id, ip.code, ip.weight, ip.target
FROM score_rules r
JOIN vendors v ON v.id = r.vendor_id
LEFT JOIN sites s ON s.id = r.site_id
JOIN rule_plan rp ON rp.kind = r.kind AND rp.vendor_code = v.code
                 AND rp.site_code IS NOT DISTINCT FROM s.code
JOIN item_plan ip ON ip.item_set = rp.item_set
WHERE NOT EXISTS (SELECT 1 FROM score_rule_items i WHERE i.rule_id = r.id);

-- Level default untuk aturan maturity
INSERT INTO score_rule_levels (rule_id, level, min_score, min_dimension, consecutive_periods)
SELECT r.id, l.level, l.min_score, l.min_dimension, l.periods
FROM score_rules r
CROSS JOIN (VALUES (2, 60, 50, 1), (3, 75, 65, 2), (4, 85, 75, 3), (5, 95, 85, 3))
    AS l(level, min_score, min_dimension, periods)
WHERE r.kind = 'MATURITY'
  AND NOT EXISTS (SELECT 1 FROM score_rule_levels x WHERE x.rule_id = r.id);

-- Ringkasan
SELECT r.kind, v.code AS vendor, coalesce(s.code, '(default)') AS site, r.period_type,
       (SELECT string_agg(i.code || ' w' || i.weight || coalesce(' t' || i.target, ''), ', ' ORDER BY i.weight DESC, i.code)
          FROM score_rule_items i WHERE i.rule_id = r.id) AS items
FROM score_rules r
JOIN vendors v ON v.id = r.vendor_id
LEFT JOIN sites s ON s.id = r.site_id
WHERE r.superseded_at IS NULL
ORDER BY r.kind DESC, v.code, s.code NULLS FIRST;

COMMIT;
