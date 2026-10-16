-- =====================================================================
-- E2E RESET (development/testing only) - DIHASILKAN OTOMATIS, jangan diedit.
-- Sumber: 007_scoring.sql + seed_v3.sql (+ kolom 008) + seed_scoring.sql (+ akhir 006).
-- Buat ulang dengan: make e2e-sql
--
-- PERINGATAN: menghapus SEMUA data di database ini lalu mengisi ulang.
--
-- Cara pakai:
--   DBeaver : buka file ini, jalankan sebagai script (Alt+X).
--             Kalau muncul "transaction aborted", jalankan ROLLBACK; dulu.
--   psql    : make e2e-reset   (sekaligus menjalankan scoring backfill)
--
-- Setelah selesai, jalankan server (make run). Job scoring langsung
-- menghitung fakta harian dan membekukan periode SLA/maturity yang sudah
-- selesai. Panduan akun & skenario: docs/E2E-TEST-DATA.md
-- =====================================================================

-- ===== 1/4: tabel SLA & maturity (007_scoring.sql) =====
BEGIN;

-- Versioned SLA / maturity rules. site_id NULL = vendor default, set = site override.
CREATE TABLE IF NOT EXISTS score_rules (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind          varchar(10) NOT NULL CHECK (kind IN ('SLA', 'MATURITY')),
    vendor_id     uuid NOT NULL REFERENCES vendors(id) ON DELETE RESTRICT,
    site_id       uuid REFERENCES sites(id) ON DELETE RESTRICT,
    period_type   varchar(10) NOT NULL CHECK (period_type IN ('WEEK', 'MONTH', 'QUARTER')),
    version       int NOT NULL CHECK (version > 0),
    created_by    uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    superseded_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_score_rules_version ON score_rules
    (kind, vendor_id, coalesce(site_id, '00000000-0000-0000-0000-000000000000'::uuid), version);
CREATE UNIQUE INDEX IF NOT EXISTS uq_score_rules_active ON score_rules
    (kind, vendor_id, coalesce(site_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS score_rule_items (
    rule_id uuid NOT NULL REFERENCES score_rules(id) ON DELETE CASCADE,
    code    varchar(40) NOT NULL,
    weight  numeric(5,2) NOT NULL CHECK (weight > 0 AND weight <= 100),
    target  numeric(10,2),
    PRIMARY KEY (rule_id, code)
);

CREATE TABLE IF NOT EXISTS score_rule_levels (
    rule_id             uuid NOT NULL REFERENCES score_rules(id) ON DELETE CASCADE,
    level               smallint NOT NULL CHECK (level BETWEEN 2 AND 5),
    min_score           numeric(5,2) NOT NULL,
    min_dimension       numeric(5,2) NOT NULL,
    consecutive_periods smallint NOT NULL CHECK (consecutive_periods >= 1),
    PRIMARY KEY (rule_id, level)
);

-- One row per task (assignment) per day, written once the day can no longer change.
CREATE TABLE IF NOT EXISTS task_day_facts (
    assignment_id         uuid NOT NULL REFERENCES pipeline_item_assignments(id) ON DELETE RESTRICT,
    report_date           date NOT NULL,
    vendor_id             uuid NOT NULL,
    site_id               uuid NOT NULL,
    pipeline_id           uuid NOT NULL,
    worker_id             uuid NOT NULL,
    reported              boolean NOT NULL,
    completed             boolean NOT NULL,
    on_time               boolean NOT NULL,
    late_minutes          int,
    late_upload           boolean NOT NULL,
    evidence_complete     boolean NOT NULL,
    k3_finding            boolean NOT NULL,
    k3_finding_with_notes boolean NOT NULL,
    finalized_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (assignment_id, report_date)
);
CREATE INDEX IF NOT EXISTS idx_task_day_facts_vendor_date ON task_day_facts(vendor_id, report_date);
CREATE INDEX IF NOT EXISTS idx_task_day_facts_site_date ON task_day_facts(site_id, report_date);
CREATE INDEX IF NOT EXISTS idx_task_day_facts_worker_date ON task_day_facts(worker_id, report_date);

-- How far task_day_facts has been finalized. The single boundary between stored
-- and live facts, and the last date a period may be frozen through.
CREATE TABLE IF NOT EXISTS scoring_job_state (
    id            smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    facts_through date
);

-- Frozen results per closed period.
CREATE TABLE IF NOT EXISTS score_period_results (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         varchar(10) NOT NULL CHECK (kind IN ('SLA', 'MATURITY')),
    rule_id      uuid NOT NULL REFERENCES score_rules(id) ON DELETE RESTRICT,
    scope_type   varchar(10) NOT NULL CHECK (scope_type IN ('VENDOR', 'SITE', 'WORKER')),
    vendor_id    uuid NOT NULL REFERENCES vendors(id) ON DELETE RESTRICT,
    site_id      uuid REFERENCES sites(id) ON DELETE RESTRICT,
    worker_id    uuid REFERENCES users(id) ON DELETE RESTRICT,
    period_type  varchar(10) NOT NULL,
    period_start date NOT NULL,
    period_end   date NOT NULL,
    status       varchar(10) NOT NULL CHECK (status IN ('OK', 'NO_DATA')),
    score        numeric(5,2),
    compliant    boolean,
    level        smallint,
    task_count   int NOT NULL DEFAULT 0,
    computed_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_score_period_results ON score_period_results (
    kind, scope_type, vendor_id,
    coalesce(site_id, '00000000-0000-0000-0000-000000000000'::uuid),
    coalesce(worker_id, '00000000-0000-0000-0000-000000000000'::uuid),
    period_type, period_start);
CREATE INDEX IF NOT EXISTS idx_score_period_results_lookup
    ON score_period_results(kind, scope_type, period_start DESC);

CREATE TABLE IF NOT EXISTS score_period_item_results (
    result_id uuid NOT NULL REFERENCES score_period_results(id) ON DELETE CASCADE,
    code      varchar(40) NOT NULL,
    actual    numeric(12,2),
    target    numeric(10,2),
    score     numeric(5,2),
    weight    numeric(5,2) NOT NULL,
    met       boolean,
    is_na     boolean NOT NULL,
    PRIMARY KEY (result_id, code)
);

COMMIT;

-- ===== 2/4: reset & isi data (seed_v3.sql) =====
-- =====================================================
-- INITIAL LOAD (development/testing, JANGAN dipakai di produksi)
-- Jalankan SETELAH init.sql (dan migrations). Script ini MENGHAPUS semua data
-- lalu mengisi ulang: master role & shift, 10 PLTU, 5 vendor, superadmin,
-- 1 admin per PLTU, 1 admin worker per vendor, 10 worker per vendor,
-- 9 pipeline area (55 item lokasi) per site vendor, assignment task tiap worker,
-- pipeline report sejak awal kuartal lalu (detail + history, tanpa review admin),
-- (aturan SLA & maturity: seed_scoring.sql; fakta & snapshot: job server / make scoring-backfill),
-- inventory dan laporannya.
-- Password semua user: Password123!
-- =====================================================

BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

TRUNCATE TABLE
    score_period_item_results,
    score_period_results,
    score_rule_levels,
    score_rule_items,
    score_rules,
    task_day_facts,
    scoring_job_state,
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

-- Zona waktu IANA per site (migration 008); jam shift & report_date dibaca di zona site
ALTER TABLE sites ADD COLUMN IF NOT EXISTS timezone varchar(64) NOT NULL DEFAULT 'Asia/Jakarta';

INSERT INTO shifts (shift, start_time, end_time) VALUES
    ('DAY',     '08:00', '16:00'),
    ('EVENING', '16:00', '00:00'),
    ('NIGHT',   '00:00', '08:00');

-- -----------------------------------------------------
-- sites: 10 PLTU
-- -----------------------------------------------------
-- Semua PLTU ini di Jawa (WIB). Site di Kalimantan/Sulawesi/Bali pakai 'Asia/Makassar' (WITA),
-- di Papua/Maluku 'Asia/Jayapura' (WIT).
INSERT INTO sites (code, name, site_type, timezone) VALUES
    ('PAITON',    'PLTU Paiton',          'PLTU', 'Asia/Jakarta'),
    ('SURALAYA',  'PLTU Suralaya',        'PLTU', 'Asia/Jakarta'),
    ('PACITAN',   'PLTU Pacitan',         'PLTU', 'Asia/Jakarta'),
    ('TJB',       'PLTU Tanjung Jati B',  'PLTU', 'Asia/Jakarta'),
    ('CIREBON',   'PLTU Cirebon',         'PLTU', 'Asia/Jakarta'),
    ('REMBANG',   'PLTU Rembang',         'PLTU', 'Asia/Jakarta'),
    ('LABUAN',    'PLTU Labuan',          'PLTU', 'Asia/Jakarta'),
    ('PLBRATU',   'PLTU Pelabuhan Ratu',  'PLTU', 'Asia/Jakarta'),
    ('ADIPALA',   'PLTU Adipala',         'PLTU', 'Asia/Jakarta'),
    ('INDRAMAYU', 'PLTU Indramayu',       'PLTU', 'Asia/Jakarta');

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

-- Admin regional: satu admin memegang 2 site (Paiton & Suralaya) untuk uji akses lintas vendor
INSERT INTO users (username, password_hash, full_name, phone, must_change_password)
VALUES ('admin.regional', crypt('Password123!', gen_salt('bf', 10)), 'Admin Regional Jawa Timur-Banten',
        '081210000099', false);

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

-- ADMIN regional -> 2 site
INSERT INTO user_scopes (user_id, site_id)
SELECT u.id, s.id
FROM users u
JOIN sites s ON s.code IN ('PAITON', 'SURALAYA')
WHERE u.username = 'admin.regional';

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
SELECT pi.id, w.id, np.created_by, (date_trunc('quarter', current_date) - interval '3 months')::date, (date_trunc('quarter', current_date) - interval '3 months')::date
FROM users w
JOIN user_scopes ws ON ws.user_id = w.id
JOIN numbered_pipelines np ON np.vendor_id = ws.vendor_id
    AND np.site_no = CASE WHEN substring(w.username FROM 10 FOR 2)::int <= 5 THEN 1 ELSE 2 END
    AND np.pipeline_no = (substring(w.username FROM 10 FOR 2)::int - 1) % 5 + 1
JOIN pipeline_items pi ON pi.pipeline_id = np.id
WHERE w.username LIKE 'w.vnd%';

-- =====================================================
-- FLOW WORKER: pipeline report dari awal kuartal lalu sampai hari ini
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
--   hari ini       : shift yang sudah selesai = seperti hari sebelumnya;
--                    shift yang masih berjalan = 40% ON_TIME, 30% NOT_FINISHED, 30% belum lapor
-- =====================================================
CREATE TEMP TABLE report_plan ON COMMIT DROP AS
WITH base AS (
    SELECT a.id AS assignment_id, a.user_id AS worker_id,
           d::date AS report_date, st.timezone AS tz,
           (ARRAY['DAY', 'EVENING', 'NIGHT'])[(substring(w.username FROM 10 FOR 2)::int - 1) % 3 + 1] AS shift,
           -- Profil vendor supaya SLA & maturity bervariasi (bucket makin besar = makin buruk):
           --   VND-001 sangat baik (SLA tercapai), VND-004 sekitar target, VND-002 membaik,
           --   VND-003 memburuk, VND-005 lemah. Tanpa offset: ~91% selesai, ~87% tepat waktu.
           -- Deterministik: sama untuk worker, area, item, dan tanggal yang sama di setiap reset.
           greatest(0, least(99, abs(hashtext(w.username || '|' || pa.name || '|' || pia.sort_order::text
                                             || '|' || d::date::text)::bigint) % 100
             + CASE substring(v.code FROM 5)::int
                   WHEN 1 THEN -12                                              -- sangat baik
                   WHEN 2 THEN least(25, (current_date - d::date) / 4) - 10     -- membaik
                   WHEN 3 THEN 15 - least(25, (current_date - d::date) / 4)     -- memburuk
                   WHEN 4 THEN -4                                               -- sekitar target
                   ELSE 18                                                      -- lemah
               END)) AS bucket
    FROM pipeline_item_assignments a
    JOIN users w ON w.id = a.user_id
    JOIN pipeline_items pia ON pia.id = a.pipeline_item_id
    JOIN pipelines pa ON pa.id = pia.pipeline_id
    JOIN vendors v ON v.id = pa.vendor_id
    JOIN sites st ON st.id = pa.site_id
    -- Dari awal kuartal lalu sampai hari ini (di zona site): ada minggu, bulan, dan 1 kuartal penuh yang sudah selesai.
    CROSS JOIN generate_series((date_trunc('quarter', current_date) - interval '3 months')::date,
                               (now() AT TIME ZONE st.timezone)::date, interval '1 day') AS d
),
planned AS (
    -- Hari sebelumnya dan shift hari ini yang sudah selesai saat seed dijalankan dianggap
    -- hari kerja normal; hanya shift hari ini yang masih berjalan yang belum banyak lapor.
    SELECT b.*,
           CASE WHEN b.report_date < (now() AT TIME ZONE b.tz)::date OR (
                    (b.report_date + sh.end_time)
                      + CASE WHEN sh.end_time <= sh.start_time THEN interval '1 day' ELSE interval '0' END
                ) <= now() AT TIME ZONE b.tz THEN
               CASE WHEN b.bucket < 65 THEN 'ON_TIME'
                    WHEN b.bucket < 73 THEN 'LATE_UPLOAD'
                    WHEN b.bucket < 80 THEN 'PARTIAL_LATE'
                    WHEN b.bucket < 87 THEN 'REVISED'
                    WHEN b.bucket < 91 THEN 'LATE_WORK'
                    WHEN b.bucket < 95 THEN 'NOT_FINISHED'
               END
           ELSE
               CASE WHEN b.bucket < 40 THEN 'ON_TIME'
                    WHEN b.bucket < 70 THEN 'NOT_FINISHED'
               END
           END AS scenario
    FROM base b
    JOIN shifts sh ON sh.shift = b.shift
)
SELECT p.*,
       (p.report_date + sh.start_time) AT TIME ZONE p.tz AS started_at,
       ((p.report_date + sh.end_time)
           + CASE WHEN sh.end_time <= sh.start_time THEN interval '1 day' ELSE interval '0' END
       ) AT TIME ZONE p.tz AS shift_end,
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

-- Aturan contoh SLA & maturity ada di seed_scoring.sql (dijalankan oleh make db-reset).

COMMIT;

-- ===== 3/4: aturan SLA & maturity (seed_scoring.sql) =====
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

-- ===== 4/4: pipelines.site_id wajib (akhir 006_pipeline_site.sql) =====
-- Semua pipeline dari seed sudah punya site, jadi kolom bisa dijadikan NOT NULL.
BEGIN;
ALTER TABLE pipelines ALTER COLUMN site_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_pipelines_site_id ON pipelines(site_id);
COMMIT;

-- Cek cepat: site_id_nullable harus NO, jumlah data harus terisi.
SELECT (SELECT is_nullable FROM information_schema.columns
        WHERE table_name = 'pipelines' AND column_name = 'site_id') AS site_id_nullable,
       (SELECT string_agg(DISTINCT timezone, ', ') FROM sites) AS site_timezones,
       (SELECT count(*) FROM users)                     AS users,
       (SELECT count(*) FROM pipeline_item_assignments) AS assignments,
       (SELECT count(*) FROM pipeline_reports)          AS reports,
       (SELECT count(*) FROM score_rules)               AS rules;
