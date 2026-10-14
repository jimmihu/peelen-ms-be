-- =====================================================
-- INITIAL LOAD (development/testing, JANGAN dipakai di produksi)
-- Jalankan SETELAH init.sql (dan migrations). Script ini MENGHAPUS semua data
-- lalu mengisi ulang: master role & shift, 10 PLTU, 5 vendor, superadmin,
-- 1 admin per PLTU, 1 admin worker per vendor, 10 worker per vendor,
-- 9 pipeline area (55 item lokasi) per site vendor, assignment task tiap worker,
-- pipeline report 7 hari terakhir (detail + history, tanpa review admin),
-- inventory dan laporannya.
-- Password semua user: Password123!
-- =====================================================

BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

TRUNCATE TABLE
    inventory_reports,
    pipeline_report_history,
    pipeline_report_details,
    pipeline_reports,
    pipeline_item_assignments,
    pipeline_items,
    pipelines,
    items,
    policies,
    inventories,
    vendor_sites,
    audit_logs,
    refresh_tokens,
    sessions,
    user_scopes,
    user_roles,
    users,
    roles,
    shifts,
    vendors,
    sites
RESTART IDENTITY CASCADE;

-- -----------------------------------------------------
-- Master data
-- -----------------------------------------------------
INSERT INTO roles (id, code, name) VALUES
    (1, 'SUPER_ADMIN',  'Super Admin'),
    (2, 'ADMIN',        'Admin'),
    (3, 'ADMIN_WORKER', 'Admin Worker'),
    (4, 'WORKER',       'Worker');

-- Samakan check constraint dengan init.sql untuk DB yang belum menjalankan migration 004
ALTER TABLE shifts
    DROP CONSTRAINT IF EXISTS shifts_shift_check,
    ADD CONSTRAINT shifts_shift_check CHECK (shift IN ('DAY', 'EVENING', 'NIGHT'));

-- Samakan check constraint status report dengan init.sql (migration 005)
ALTER TABLE pipeline_reports
    DROP CONSTRAINT IF EXISTS pipeline_reports_status_check,
    DROP CONSTRAINT IF EXISTS pipeline_reports_status_revisi_check,
    ADD CONSTRAINT pipeline_reports_status_check CHECK (status IN ('IN_PROGRESS', 'COMPLETED')),
    ADD CONSTRAINT pipeline_reports_status_revisi_check CHECK (status_revisi IN ('NONE', 'SUBMITTED'));
ALTER TABLE pipeline_report_history
    DROP CONSTRAINT IF EXISTS pipeline_report_history_new_status_check,
    DROP CONSTRAINT IF EXISTS pipeline_report_history_new_revision_check,
    ADD CONSTRAINT pipeline_report_history_new_status_check CHECK (new_status IN ('IN_PROGRESS', 'COMPLETED')),
    ADD CONSTRAINT pipeline_report_history_new_revision_check CHECK (new_revision IN ('NONE', 'SUBMITTED'));

-- Pipelines belong to a site (migration 006)
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS site_id uuid REFERENCES sites(id);

INSERT INTO shifts (shift, start_time, end_time) VALUES
    ('DAY',     '08:00', '16:00'),
    ('EVENING', '16:00', '00:00'),
    ('NIGHT',   '00:00', '08:00');

-- -----------------------------------------------------
-- sites: 10 PLTU
-- -----------------------------------------------------
INSERT INTO sites (code, name, site_type) VALUES
    ('PAITON',    'PLTU Paiton',          'PLTU'),
    ('SURALAYA',  'PLTU Suralaya',        'PLTU'),
    ('PACITAN',   'PLTU Pacitan',         'PLTU'),
    ('TJB',       'PLTU Tanjung Jati B',  'PLTU'),
    ('CIREBON',   'PLTU Cirebon',         'PLTU'),
    ('REMBANG',   'PLTU Rembang',         'PLTU'),
    ('LABUAN',    'PLTU Labuan',          'PLTU'),
    ('PLBRATU',   'PLTU Pelabuhan Ratu',  'PLTU'),
    ('ADIPALA',   'PLTU Adipala',         'PLTU'),
    ('INDRAMAYU', 'PLTU Indramayu',       'PLTU');

-- -----------------------------------------------------
-- vendors: 5 vendor, masing-masing pegang 2 PLTU
-- -----------------------------------------------------
INSERT INTO vendors (code, name) VALUES
    ('VND-001', 'PT Mitra Teknik Abadi'),
    ('VND-002', 'PT Karya Mandiri Sejahtera'),
    ('VND-003', 'CV Sinergi Perkasa'),
    ('VND-004', 'PT Bumi Jaya Konstruksi'),
    ('VND-005', 'PT Cahaya Energi Nusantara');

INSERT INTO vendor_sites (vendor_id, site_id)
SELECT v.id, s.id
FROM (VALUES
    ('VND-001', 'PAITON'),
    ('VND-001', 'PACITAN'),
    ('VND-002', 'SURALAYA'),
    ('VND-002', 'LABUAN'),
    ('VND-003', 'TJB'),
    ('VND-003', 'REMBANG'),
    ('VND-004', 'CIREBON'),
    ('VND-004', 'INDRAMAYU'),
    ('VND-005', 'PLBRATU'),
    ('VND-005', 'ADIPALA')
) AS mapping(vendor_code, site_code)
JOIN vendors v ON v.code = mapping.vendor_code
JOIN sites s ON s.code = mapping.site_code;

-- -----------------------------------------------------
-- users
-- -----------------------------------------------------
-- Super admin
INSERT INTO users (username, password_hash, full_name, email, phone, must_change_password)
VALUES ('superadmin', crypt('Password123!', gen_salt('bf', 10)), 'Budi Santoso',
        'budi.santoso@example.com', '081200000001', false);

-- Admin: 1 per PLTU, username admin.<kode site>
INSERT INTO users (username, password_hash, full_name, phone, must_change_password)
SELECT 'admin.' || lower(s.code), crypt('Password123!', gen_salt('bf', 10)),
       'Admin ' || s.name, '0812100000' || lpad(row_number() OVER (ORDER BY s.code)::text, 2, '0'), false
FROM sites s;

-- Admin worker: 1 per vendor
INSERT INTO users (username, password_hash, full_name, phone, must_change_password) VALUES
    ('aw.mitra',   crypt('Password123!', gen_salt('bf', 10)), 'Rudi Hartono',   '081220000001', false),
    ('aw.karya',   crypt('Password123!', gen_salt('bf', 10)), 'Ani Wulandari',  '081220000002', false),
    ('aw.sinergi', crypt('Password123!', gen_salt('bf', 10)), 'Joko Susilo',    '081220000003', false),
    ('aw.bumi',    crypt('Password123!', gen_salt('bf', 10)), 'Lukman Hakim',   '081220000004', false),
    ('aw.cahaya',  crypt('Password123!', gen_salt('bf', 10)), 'Maya Anggraini', '081220000005', false);

-- Worker: 10 per vendor, username w.vnd001.01 .. w.vnd005.10
INSERT INTO users (username, password_hash, full_name, phone, must_change_password)
SELECT 'w.' || lower(replace(v.code, '-', '')) || '.' || lpad(n::text, 2, '0'),
       crypt('Password123!', gen_salt('bf', 10)),
       (ARRAY['Andi', 'Bayu', 'Candra', 'Dimas', 'Eka', 'Fikri', 'Galih', 'Hadi', 'Ilham', 'Jaka'])[n]
           || ' ' ||
       (ARRAY['Saputra', 'Wijaya', 'Hidayat', 'Kusuma', 'Pratama'])[substring(v.code FROM 5)::int],
       '08130' || substring(v.code FROM 5) || lpad(n::text, 4, '0'),
       false
FROM vendors v
CROSS JOIN generate_series(1, 10) AS n;

-- -----------------------------------------------------
-- user_roles
-- -----------------------------------------------------
INSERT INTO user_roles (user_id, role_id, assigned_by)
SELECT u.id, r.id,
       CASE WHEN u.username = 'superadmin' THEN NULL ELSE sa.id END
FROM users u
JOIN roles r ON r.code = CASE
        WHEN u.username = 'superadmin'   THEN 'SUPER_ADMIN'
        WHEN u.username LIKE 'admin.%'   THEN 'ADMIN'
        WHEN u.username LIKE 'aw.%'      THEN 'ADMIN_WORKER'
        ELSE 'WORKER'
    END
CROSS JOIN (SELECT id FROM users WHERE username = 'superadmin') sa;

-- -----------------------------------------------------
-- user_scopes (SUPER_ADMIN tidak punya scope)
-- -----------------------------------------------------
-- ADMIN -> site
INSERT INTO user_scopes (user_id, site_id)
SELECT u.id, s.id
FROM sites s
JOIN users u ON u.username = 'admin.' || lower(s.code);

-- ADMIN_WORKER -> vendor
INSERT INTO user_scopes (user_id, vendor_id)
SELECT u.id, v.id
FROM (VALUES
    ('aw.mitra',   'VND-001'),
    ('aw.karya',   'VND-002'),
    ('aw.sinergi', 'VND-003'),
    ('aw.bumi',    'VND-004'),
    ('aw.cahaya',  'VND-005')
) AS m(username, vendor_code)
JOIN users u   ON u.username = m.username
JOIN vendors v ON v.code = m.vendor_code;

-- WORKER -> vendor (diambil dari username w.vnd001.xx)
INSERT INTO user_scopes (user_id, vendor_id)
SELECT u.id, v.id
FROM users u
JOIN vendors v ON v.code = 'VND-' || substring(u.username FROM 6 FOR 3)
WHERE u.username LIKE 'w.vnd%';

-- -----------------------------------------------------
-- Pipeline (area) dan item (lokasi) PLTU.
-- Semua vendor mendapat set pipeline yang sama.
-- -----------------------------------------------------
CREATE TEMP TABLE pipeline_fixture (pipeline_name text, sort_order int, item_name text) ON COMMIT DROP;

INSERT INTO pipeline_fixture (pipeline_name, sort_order, item_name) VALUES
    ('Ash Handling System',           1,  'Ash Conveyor'),
    ('Ash Handling System',           2,  'Ash Hopper'),
    ('Ash Handling System',           3,  'Bottom Ash Handling'),
    ('Ash Handling System',           4,  'Fly Ash Silo'),
    ('Boiler Area',                   1,  'Area Pulverizer'),
    ('Boiler Area',                   2,  'Lantai Boiler House'),
    ('Boiler Area',                   3,  'Platform Boiler Lantai 1'),
    ('Boiler Area',                   4,  'Platform Boiler Lantai 2'),
    ('Boiler Area',                   5,  'Tangga Boiler'),
    ('Building & Control Facilities', 1,  'Control Room'),
    ('Building & Control Facilities', 2,  'Electrical Building'),
    ('Building & Control Facilities', 3,  'Gedung Administrasi'),
    ('Building & Control Facilities', 4,  'Gudang Maintenance'),
    ('Building & Control Facilities', 5,  'Koridor Lantai 1'),
    ('Building & Control Facilities', 6,  'Koridor Lantai 2'),
    ('Building & Control Facilities', 7,  'Lobby Utama'),
    ('Building & Control Facilities', 8,  'Ruang Ganti'),
    ('Building & Control Facilities', 9,  'Tangga Gedung'),
    ('Building & Control Facilities', 10, 'Toilet Pria'),
    ('Building & Control Facilities', 11, 'Toilet Wanita'),
    ('Building & Control Facilities', 12, 'Workshop'),
    ('Coal Handling System',          1,  'Coal Bunker'),
    ('Coal Handling System',          2,  'Conveyor Gallery A'),
    ('Coal Handling System',          3,  'Conveyor Walkway'),
    ('Coal Handling System',          4,  'Crusher House'),
    ('Coal Handling System',          5,  'Screening House'),
    ('Coal Handling System',          6,  'Transfer Tower 1'),
    ('Coal Handling System',          7,  'Transfer Tower 2'),
    ('Coal Yard',                     1,  'Area Stacker-Reclaimer'),
    ('Coal Yard',                     2,  'Coal Stockpile'),
    ('Coal Yard',                     3,  'Jalur Akses Coal Yard'),
    ('Coal Yard',                     4,  'Saluran Drainase Coal Yard'),
    ('Flue Gas & Emission Area',      1,  'Area Bag Filter'),
    ('Flue Gas & Emission Area',      2,  'Area Ducting'),
    ('Flue Gas & Emission Area',      3,  'Area ESP'),
    ('Flue Gas & Emission Area',      4,  'Area FGD'),
    ('Road, Drainage & General Area', 1,  'Area Bongkar Muat'),
    ('Road, Drainage & General Area', 2,  'Area Pagar Perimeter'),
    ('Road, Drainage & General Area', 3,  'Area Parkir Mobil'),
    ('Road, Drainage & General Area', 4,  'Area Parkir Motor'),
    ('Road, Drainage & General Area', 5,  'Area Tempat Sampah'),
    ('Road, Drainage & General Area', 6,  'Jalan Hauling Batu Bara'),
    ('Road, Drainage & General Area', 7,  'Jalan Operasional'),
    ('Road, Drainage & General Area', 8,  'Jalur Pedestrian'),
    ('Road, Drainage & General Area', 9,  'Pos Keamanan'),
    ('Road, Drainage & General Area', 10, 'Saluran Drainase'),
    ('Turbine Hall',                  1,  'Area Generator'),
    ('Turbine Hall',                  2,  'Area Sekitar Turbin'),
    ('Turbine Hall',                  3,  'Lantai Utama Turbine Hall'),
    ('Turbine Hall',                  4,  'Platform Lantai Atas'),
    ('Turbine Hall',                  5,  'Tangga Turbine Hall'),
    ('Water Treatment & Utility',     1,  'Area Chemical Dosing'),
    ('Water Treatment & Utility',     2,  'Area Pompa'),
    ('Water Treatment & Utility',     3,  'Area Tangki Utilitas'),
    ('Water Treatment & Utility',     4,  'Water Treatment Building');

-- items: master lokasi, dipakai lintas vendor
INSERT INTO items (name, created_by)
SELECT DISTINCT f.item_name, sa.id
FROM pipeline_fixture f
CROSS JOIN (SELECT id FROM users WHERE username = 'superadmin') sa;

-- pipelines: 9 area per site vendor, dibuat oleh admin worker vendor tsb
INSERT INTO pipelines (vendor_id, site_id, name, created_by)
SELECT v.id, vs.site_id, f.pipeline_name, aw.id
FROM vendors v
JOIN vendor_sites vs ON vs.vendor_id = v.id
CROSS JOIN (SELECT DISTINCT pipeline_name FROM pipeline_fixture) f
JOIN user_scopes us ON us.vendor_id = v.id
JOIN users aw ON aw.id = us.user_id AND aw.username LIKE 'aw.%';

INSERT INTO pipeline_items (pipeline_id, item_id, sort_order, created_by)
SELECT p.id, i.id, f.sort_order, p.created_by
FROM pipeline_fixture f
JOIN pipelines p ON p.name = f.pipeline_name
JOIN items i ON i.name = f.item_name;

-- -----------------------------------------------------
-- pipeline_item_assignments
-- Worker 01-05 tiap vendor kerja di site pertama vendor (urut kode site),
-- worker 06-10 di site kedua. Di tiap site, worker ke-n (1..5) dapat semua item
-- area ke-n (area diurutkan per nama). Di-assign oleh admin worker vendor tsb.
-- -----------------------------------------------------
WITH vendor_site_no AS (
    SELECT vs.vendor_id, vs.site_id,
           row_number() OVER (PARTITION BY vs.vendor_id ORDER BY s.code) AS site_no
    FROM vendor_sites vs JOIN sites s ON s.id = vs.site_id
),
numbered_pipelines AS (
    SELECT p.*, vsn.site_no,
           row_number() OVER (PARTITION BY p.vendor_id, p.site_id ORDER BY p.name) AS pipeline_no
    FROM pipelines p
    JOIN vendor_site_no vsn ON vsn.vendor_id = p.vendor_id AND vsn.site_id = p.site_id
)
INSERT INTO pipeline_item_assignments (pipeline_item_id, user_id, assigned_by, created_at, updated_at)
SELECT pi.id, w.id, np.created_by, current_date - 100, current_date - 100
FROM users w
JOIN user_scopes ws ON ws.user_id = w.id
JOIN numbered_pipelines np ON np.vendor_id = ws.vendor_id
    AND np.site_no = CASE WHEN substring(w.username FROM 10 FOR 2)::int <= 5 THEN 1 ELSE 2 END
    AND np.pipeline_no = (substring(w.username FROM 10 FOR 2)::int - 1) % 5 + 1
JOIN pipeline_items pi ON pi.pipeline_id = np.id
WHERE w.username LIKE 'w.vnd%';

-- =====================================================
-- FLOW WORKER: pipeline report 7 hari terakhir (H-6 .. hari ini)
-- Tiap worker kerja di satu shift tetap: worker 01/04/07/10 DAY,
-- 02/05/08 EVENING, 03/06/09 NIGHT. Tidak ada review admin:
--   status        IN_PROGRESS -> COMPLETED (diubah worker)
--   status_revisi SUBMITTED bila report dikirim/diubah setelah shift selesai
-- Skenario per task per hari:
--   hari sebelumnya: 65% ON_TIME      selesai & upload dalam shift
--                     8% LATE_UPLOAD  kerja dalam shift, tidak ada sinyal sama sekali,
--                                     seluruh report dikirim 2 jam setelah shift selesai
--                     7% PARTIAL_LATE foto before terkirim dalam shift, foto after
--                                     baru terkirim 3 jam setelah shift selesai
--                     7% REVISED      selesai dalam shift, lalu worker mengganti foto
--                                     after 4 jam setelah shift selesai
--                     4% LATE_WORK    pekerjaan selesai lewat jam shift
--                     4% NOT_FINISHED report dibuat tapi tidak diselesaikan
--                     5% tidak lapor
--   hari ini       : 30% ON_TIME, 20% NOT_FINISHED (masih berjalan), 50% belum lapor
-- =====================================================
CREATE TEMP TABLE report_plan ON COMMIT DROP AS
WITH base AS (
    SELECT a.id AS assignment_id, a.user_id AS worker_id,
           d::date AS report_date,
           (ARRAY['DAY', 'EVENING', 'NIGHT'])[(substring(w.username FROM 10 FOR 2)::int - 1) % 3 + 1] AS shift,
           abs(hashtext(a.id::text || d::date::text)::bigint) % 100 AS bucket
    FROM pipeline_item_assignments a
    JOIN users w ON w.id = a.user_id
    CROSS JOIN generate_series(current_date - 6, current_date, interval '1 day') AS d
),
planned AS (
    SELECT b.*,
           CASE WHEN b.report_date < current_date THEN
               CASE WHEN b.bucket < 65 THEN 'ON_TIME'
                    WHEN b.bucket < 73 THEN 'LATE_UPLOAD'
                    WHEN b.bucket < 80 THEN 'PARTIAL_LATE'
                    WHEN b.bucket < 87 THEN 'REVISED'
                    WHEN b.bucket < 91 THEN 'LATE_WORK'
                    WHEN b.bucket < 95 THEN 'NOT_FINISHED'
               END
           ELSE
               CASE WHEN b.bucket < 30 THEN 'ON_TIME'
                    WHEN b.bucket < 50 THEN 'NOT_FINISHED'
               END
           END AS scenario
    FROM base b
)
SELECT p.*,
       (p.report_date + sh.start_time)::timestamptz AS started_at,
       ((p.report_date + sh.end_time)
           + CASE WHEN sh.end_time <= sh.start_time THEN interval '1 day' ELSE interval '0' END
       )::timestamptz AS shift_end,
       NULL::uuid AS report_id
FROM planned p
JOIN shifts sh ON sh.shift = p.shift
WHERE p.scenario IS NOT NULL;

-- Waktu tiap langkah per skenario
ALTER TABLE report_plan
    ADD COLUMN created_at   timestamptz,  -- report pertama kali diterima server
    ADD COLUMN open_date    timestamptz,  -- jam mulai kerja (dari HP)
    ADD COLUMN closed_date  timestamptz,  -- jam selesai kerja (dari HP)
    ADD COLUMN completed_at timestamptz,  -- status COMPLETED diterima server
    ADD COLUMN revised_at   timestamptz;  -- REVISED: foto after diganti setelah shift

UPDATE report_plan SET
    open_date = started_at + interval '30 minutes',
    created_at = CASE scenario WHEN 'LATE_UPLOAD' THEN shift_end + interval '2 hours'
                               ELSE started_at + interval '30 minutes' END,
    closed_date = CASE WHEN scenario IN ('ON_TIME', 'LATE_UPLOAD', 'PARTIAL_LATE', 'REVISED')
                       THEN started_at + interval '1 hour'
                       WHEN scenario = 'LATE_WORK' THEN shift_end + interval '45 minutes' END,
    completed_at = CASE scenario WHEN 'ON_TIME'      THEN started_at + interval '1 hour'
                                 WHEN 'REVISED'      THEN started_at + interval '1 hour'
                                 WHEN 'LATE_UPLOAD'  THEN shift_end + interval '2 hours'
                                 WHEN 'PARTIAL_LATE' THEN shift_end + interval '3 hours'
                                 WHEN 'LATE_WORK'    THEN shift_end + interval '1 hour' END,
    revised_at = CASE WHEN scenario = 'REVISED' THEN shift_end + interval '4 hours' END;

INSERT INTO pipeline_reports
    (pipeline_item_assignment_id, shift, report_date, status, status_revisi, created_at, updated_at)
SELECT rp.assignment_id, rp.shift, rp.report_date,
       CASE WHEN rp.completed_at IS NULL THEN 'IN_PROGRESS' ELSE 'COMPLETED' END,
       CASE WHEN rp.scenario IN ('LATE_UPLOAD', 'PARTIAL_LATE', 'REVISED', 'LATE_WORK')
            THEN 'SUBMITTED' ELSE 'NONE' END,
       rp.created_at,
       coalesce(rp.revised_at, rp.completed_at, rp.created_at)
FROM report_plan rp;

UPDATE report_plan rp
SET report_id = pr.id
FROM pipeline_reports pr
WHERE pr.pipeline_item_assignment_id = rp.assignment_id
  AND pr.shift = rp.shift
  AND pr.report_date = rp.report_date;

-- Detail: evidence foto, jam buka/tutup, dan catatan temuan K3
INSERT INTO pipeline_report_details
    (pipeline_report_id, evidence_before, evidence_after, open_date, closed_date,
     note_category, notes, created_at, updated_at)
SELECT rp.report_id,
       'pipeline-reports/' || rp.report_id || '/before.jpg',
       CASE WHEN rp.scenario = 'REVISED'
            THEN 'pipeline-reports/' || rp.report_id || '/after-revisi.jpg'
            WHEN rp.closed_date IS NOT NULL
            THEN 'pipeline-reports/' || rp.report_id || '/after.jpg' END,
       rp.open_date,
       rp.closed_date,
       CASE rp.bucket % 20
           WHEN 0 THEN 'NEAR_MISS'
           WHEN 1 THEN 'UNSAFE_ACTION'
           WHEN 2 THEN 'UNSAFE_CONDITION'
       END,
       CASE
           WHEN rp.scenario = 'LATE_UPLOAD' THEN 'Tidak ada sinyal di lokasi, laporan dikirim setelah keluar area'
           WHEN rp.scenario = 'PARTIAL_LATE' THEN 'Sinyal hilang saat ambil foto after, dikirim setelah keluar area'
           WHEN rp.scenario = 'REVISED' THEN 'Foto after diganti karena foto sebelumnya blur'
           WHEN rp.bucket % 20 = 0 THEN 'Hampir terpeleset karena ceceran oli di lantai, area sudah dibersihkan'
           WHEN rp.bucket % 20 = 1 THEN 'Ditemukan pekerja tidak memakai helm di area kerja, sudah ditegur'
           WHEN rp.bucket % 20 = 2 THEN 'Railing tangga longgar, sudah dipasang barikade sementara'
           ELSE 'Area bersih dan aman, tidak ada temuan'
       END,
       rp.created_at,
       coalesce(rp.revised_at, rp.completed_at, rp.created_at)
FROM report_plan rp;

-- History: semua perubahan dilakukan worker sendiri
INSERT INTO pipeline_report_history
    (pipeline_report_id, actor_id, previous_status, new_status,
     previous_revision, new_revision, reason, created_at)
-- 1. Report dibuat. LATE_UPLOAD langsung terkirim lengkap (COMPLETED) setelah shift selesai.
SELECT rp.report_id, rp.worker_id, NULL,
       CASE WHEN rp.scenario = 'LATE_UPLOAD' THEN 'COMPLETED' ELSE 'IN_PROGRESS' END,
       NULL,
       CASE WHEN rp.scenario = 'LATE_UPLOAD' THEN 'SUBMITTED' ELSE 'NONE' END,
       CASE WHEN rp.scenario = 'LATE_UPLOAD' THEN 'Tidak ada sinyal di lokasi' END,
       rp.created_at
FROM report_plan rp
UNION ALL
-- 2. Report diselesaikan. Kalau terkirim setelah shift selesai -> revisi SUBMITTED.
SELECT rp.report_id, rp.worker_id, 'IN_PROGRESS', 'COMPLETED',
       'NONE',
       CASE WHEN rp.scenario IN ('PARTIAL_LATE', 'LATE_WORK') THEN 'SUBMITTED' ELSE 'NONE' END,
       CASE WHEN rp.scenario = 'PARTIAL_LATE' THEN 'Tidak ada sinyal saat ambil foto after' END,
       rp.completed_at
FROM report_plan rp
WHERE rp.scenario IN ('ON_TIME', 'PARTIAL_LATE', 'REVISED', 'LATE_WORK')
UNION ALL
-- 3. REVISED: report yang sudah COMPLETED diubah setelah shift selesai
SELECT rp.report_id, rp.worker_id, 'COMPLETED', 'COMPLETED',
       'NONE', 'SUBMITTED',
       'Foto after diganti karena foto sebelumnya blur',
       rp.revised_at
FROM report_plan rp
WHERE rp.scenario = 'REVISED';

-- =====================================================
-- FLOW WORKER: inventory dan laporan inventory
-- =====================================================
INSERT INTO inventories (vendor_id, item, jenis_item, qty, status_condition, created_by, created_at, updated_at)
SELECT v.id, fixture.item, fixture.jenis_item, fixture.qty, fixture.status_condition, aw.id,
       current_date - 14, current_date - 14
FROM vendors v
JOIN user_scopes us ON us.vendor_id = v.id
JOIN users aw ON aw.id = us.user_id AND aw.username LIKE 'aw.%'
CROSS JOIN (VALUES
    ('Safety Helmet',        'APD',             40, 'GOOD'),
    ('Rompi Reflektor',      'APD',             40, 'GOOD'),
    ('Safety Shoes',         'APD',             30, 'DAMAGED'),
    ('Kacamata Safety',      'APD',             25, 'GOOD'),
    ('Masker Debu N95',      'APD',            100, 'GOOD'),
    ('APAR CO2 6 kg',        'Alat Pemadam',    12, 'MAINTENANCE'),
    ('Radio HT',             'Komunikasi',      10, 'DAMAGED'),
    ('Senter Explosion Proof', 'Peralatan',      8, 'GOOD')
) AS fixture(item, jenis_item, qty, status_condition);

-- Laporan dari worker 01-03 tiap vendor:
--   worker 01: barang rusak, sudah RESOLVED
--   worker 02: barang rusak, sedang IN_REVIEW
--   worker 03: permintaan barang baru (REQUEST), masih SUBMITTED
INSERT INTO inventory_reports
    (vendor_id, inventory_id, user_id, report_type, evidence, description,
     status, review_reason, reviewed_by, created_at, updated_at)
SELECT ws.vendor_id, inv.id, w.id, fixture.report_type,
       CASE WHEN fixture.report_type = 'DAMAGED'
            THEN 'inventory-reports/' || w.username || '/' || fixture.n || '.jpg' END,
       fixture.description, fixture.status, fixture.review_reason,
       CASE WHEN fixture.status <> 'SUBMITTED' THEN aw.id END,
       current_date - fixture.days_ago + time '09:00',
       current_date - fixture.days_ago + time '09:00'
           + CASE WHEN fixture.status <> 'SUBMITTED' THEN interval '1 day' ELSE interval '0' END
FROM (VALUES
    (1, '01', 'DAMAGED', 'Radio HT',      'Radio HT tidak bisa transmit, baterai cepat habis',
        'RESOLVED',  'Sudah diganti unit cadangan', 5),
    (2, '02', 'DAMAGED', 'Safety Shoes',  'Sol sepatu safety lepas setelah dipakai di coal yard',
        'IN_REVIEW', NULL, 2),
    (3, '03', 'REQUEST', NULL,            'Permintaan sarung tangan tahan panas untuk area boiler',
        'SUBMITTED', NULL, 1)
) AS fixture(n, worker_no, report_type, inventory_item, description, status, review_reason, days_ago)
JOIN users w ON w.username LIKE 'w.vnd%' AND substring(w.username FROM 10 FOR 2) = fixture.worker_no
JOIN user_scopes ws ON ws.user_id = w.id
JOIN user_scopes aws ON aws.vendor_id = ws.vendor_id
JOIN users aw ON aw.id = aws.user_id AND aw.username LIKE 'aw.%'
LEFT JOIN inventories inv ON inv.vendor_id = ws.vendor_id AND inv.item = fixture.inventory_item;

COMMIT;
