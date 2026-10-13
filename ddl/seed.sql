-- =====================================================
-- DATA DUMMY (untuk development/testing, JANGAN dipakai di produksi)
-- Jalankan SETELAH schema.sql
-- Password semua user dummy: Password123!
-- =====================================================

BEGIN;

-- pgcrypto: untuk hash password (bcrypt) dan hash token (sha256)
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- -----------------------------------------------------
-- sites
-- -----------------------------------------------------
INSERT INTO sites (code, name, site_type) VALUES
    ('PAITON',   'PLTU Paiton',            'PLTU'),
    ('SURALAYA', 'PLTU Suralaya',          'PLTU'),
    ('PACITAN',  'PLTU Pacitan',           'PLTU'),
    ('PBK-CLG',  'Pabrik Cilegon',         'pabrik'),
    ('GD-JKT',   'Gedung Kantor Jakarta',  'gedung');

-- -----------------------------------------------------
-- vendors
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
    ('VND-003', 'PBK-CLG'),
    ('VND-004', 'GD-JKT'),
    ('VND-005', 'GD-JKT')
) AS mapping(vendor_code, site_code)
JOIN vendors v ON v.code = mapping.vendor_code
JOIN sites s ON s.code = mapping.site_code;

-- -----------------------------------------------------
-- users  (password: Password123!)
-- -----------------------------------------------------
INSERT INTO users
    (username, password_hash, full_name, email, phone,
     must_change_password, failed_login_count, locked_until, is_active, last_login_at)
VALUES
    -- Super admin
    ('superadmin',   crypt('Password123!', gen_salt('bf', 10)), 'Budi Santoso',     'budi.santoso@example.com',   '081200000001', false, 0, NULL, true,  now() - interval '1 hour'),

    -- Admin (scope: site)
    ('admin.paiton',   crypt('Password123!', gen_salt('bf', 10)), 'Siti Rahmawati',   'siti.rahmawati@example.com', '081200000002', false, 0, NULL, true,  now() - interval '3 hours'),
    ('admin.suralaya', crypt('Password123!', gen_salt('bf', 10)), 'Agus Prasetyo',    'agus.prasetyo@example.com',  '081200000003', false, 0, NULL, true,  now() - interval '1 day'),
    ('admin.cilegon',  crypt('Password123!', gen_salt('bf', 10)), 'Dewi Lestari',     'dewi.lestari@example.com',   '081200000004', false, 0, NULL, true,  now() - interval '2 days'),

    -- Admin worker (scope: vendor)
    ('aw.mitra',     crypt('Password123!', gen_salt('bf', 10)), 'Rudi Hartono',     'rudi.hartono@example.com',   '081200000005', false, 0, NULL, true,  now() - interval '5 hours'),
    ('aw.karya',     crypt('Password123!', gen_salt('bf', 10)), 'Ani Wulandari',    'ani.wulandari@example.com',  '081200000006', false, 0, NULL, true,  now() - interval '1 day'),
    ('aw.sinergi',   crypt('Password123!', gen_salt('bf', 10)), 'Joko Susilo',      NULL,                         '081200000007', false, 0, NULL, true,  now() - interval '4 days'),

    -- Worker (scope: vendor)
    ('worker01',     crypt('Password123!', gen_salt('bf', 10)), 'Andi Saputra',     NULL,                         '081200000011', false, 0, NULL, true,  now() - interval '2 hours'),
    ('worker02',     crypt('Password123!', gen_salt('bf', 10)), 'Bambang Triyono',  NULL,                         '081200000012', false, 0, NULL, true,  now() - interval '6 hours'),
    ('worker03',     crypt('Password123!', gen_salt('bf', 10)), 'Cahyo Nugroho',    NULL,                         '081200000013', false, 0, NULL, true,  now() - interval '1 day'),
    ('worker04',     crypt('Password123!', gen_salt('bf', 10)), 'Dedi Kurniawan',   NULL,                         '081200000014', false, 0, NULL, true,  now() - interval '3 days'),
    ('worker05',     crypt('Password123!', gen_salt('bf', 10)), 'Eko Wahyudi',      NULL,                         '081200000015', false, 0, NULL, true,  now() - interval '2 days'),
    ('worker06',     crypt('Password123!', gen_salt('bf', 10)), 'Fajar Ramadhan',   NULL,                         '081200000016', false, 0, NULL, true,  now() - interval '7 days'),

    -- Kasus khusus untuk testing
    ('worker07',     crypt('Password123!', gen_salt('bf', 10)), 'Gilang Pratama',   NULL,                         '081200000017', true,  0, NULL, true,  NULL),                             -- belum ganti password
    ('worker08',     crypt('Password123!', gen_salt('bf', 10)), 'Hendra Gunawan',   NULL,                         '081200000018', false, 5, now() + interval '15 minutes', true, now() - interval '1 day'), -- terkunci
    ('worker09',     crypt('Password123!', gen_salt('bf', 10)), 'Irfan Maulana',    NULL,                         '081200000019', false, 0, NULL, false, now() - interval '30 days');          -- nonaktif

-- -----------------------------------------------------
-- user_roles
-- -----------------------------------------------------
INSERT INTO user_roles (user_id, role_id, assigned_by)
SELECT u.id, r.id, sa.id
FROM (VALUES
    ('superadmin',     'SUPER_ADMIN'),
    ('admin.paiton',   'ADMIN'),
    ('admin.suralaya', 'ADMIN'),
    ('admin.cilegon',  'ADMIN'),
    ('aw.mitra',       'ADMIN_WORKER'),
    ('aw.karya',       'ADMIN_WORKER'),
    ('aw.sinergi',     'ADMIN_WORKER'),
    ('worker01',       'WORKER'),
    ('worker02',       'WORKER'),
    ('worker03',       'WORKER'),
    ('worker04',       'WORKER'),
    ('worker05',       'WORKER'),
    ('worker06',       'WORKER'),
    ('worker07',       'WORKER'),
    ('worker08',       'WORKER'),
    ('worker09',       'WORKER')
) AS m(username, role_code)
JOIN users u  ON u.username = m.username
JOIN roles r  ON r.code = m.role_code
CROSS JOIN (SELECT id FROM users WHERE username = 'superadmin') sa
-- superadmin tidak di-assign oleh dirinya sendiri
WHERE u.username <> 'superadmin'
UNION ALL
SELECT u.id, r.id, NULL
FROM users u JOIN roles r ON r.code = 'SUPER_ADMIN'
WHERE u.username = 'superadmin';

-- aw.mitra juga merangkap WORKER (contoh multi-role)
INSERT INTO user_roles (user_id, role_id, assigned_by)
SELECT u.id, r.id, sa.id
FROM users u, roles r, users sa
WHERE u.username = 'aw.mitra' AND r.code = 'WORKER' AND sa.username = 'superadmin';

-- -----------------------------------------------------
-- user_scopes  (SUPER_ADMIN tidak punya baris scope)
-- -----------------------------------------------------
-- ADMIN -> site
INSERT INTO user_scopes (user_id, site_id)
SELECT u.id, s.id
FROM (VALUES
    ('admin.paiton',   'PAITON'),
    ('admin.paiton',   'PACITAN'),      -- satu admin bisa pegang >1 site
    ('admin.suralaya', 'SURALAYA'),
    ('admin.cilegon',  'PBK-CLG'),
    ('admin.cilegon',  'GD-JKT')
) AS m(username, site_code)
JOIN users u ON u.username = m.username
JOIN sites s ON s.code = m.site_code;

-- ADMIN_WORKER & WORKER -> vendor
INSERT INTO user_scopes (user_id, vendor_id)
SELECT u.id, v.id
FROM (VALUES
    ('aw.mitra',   'VND-001'),
    ('aw.karya',   'VND-002'),
    ('aw.sinergi', 'VND-003'),
    ('worker01',   'VND-001'),
    ('worker02',   'VND-001'),
    ('worker03',   'VND-002'),
    ('worker04',   'VND-002'),
    ('worker05',   'VND-003'),
    ('worker06',   'VND-004'),
    ('worker07',   'VND-005'),
    ('worker08',   'VND-001'),
    ('worker09',   'VND-004')
) AS m(username, vendor_code)
JOIN users u   ON u.username = m.username
JOIN vendors v ON v.code = m.vendor_code;

-- -----------------------------------------------------
-- Dummy sessions and their associated refresh tokens
-- -----------------------------------------------------
WITH token_data(username, n, expires_at, revoked_at, user_agent, ip) AS (
    VALUES
        ('superadmin',   1, now() + interval '7 days',  NULL::timestamptz,           'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/126.0',          '192.168.1.10'),
        ('admin.paiton', 1, now() + interval '6 days',  NULL::timestamptz,           'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Firefox/127.0',         '192.168.1.21'),
        ('admin.paiton', 2, now() - interval '1 day',   now() - interval '2 days',   'Mozilla/5.0 (Linux; Android 14) Chrome/126.0 Mobile',             '10.0.0.5'),
        ('worker01',     1, now() + interval '5 days',  NULL::timestamptz,           'Mozilla/5.0 (Linux; Android 13) Chrome/125.0 Mobile',             '10.0.0.34'),
        ('worker02',     1, now() - interval '3 days',  NULL::timestamptz,           'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5) Safari/604.1',           '10.0.0.48')
),
inserted_sessions AS (
    INSERT INTO sessions (user_id, token_hash, expires_at, revoked_at, user_agent, ip_address)
    SELECT u.id,
           encode(digest('dummy-access-' || t.username || '-' || t.n, 'sha256'), 'hex'),
           t.expires_at, t.revoked_at, t.user_agent, t.ip::inet
    FROM token_data t
    JOIN users u ON u.username = t.username
    RETURNING id, user_id, token_hash
)
INSERT INTO refresh_tokens (user_id, session_id, token_hash, expires_at, revoked_at, user_agent, ip_address)
SELECT s.user_id, s.id,
       encode(digest('dummy-token-' || t.username || '-' || t.n, 'sha256'), 'hex'),
       t.expires_at, t.revoked_at, t.user_agent, t.ip::inet
FROM inserted_sessions s
JOIN token_data t
  ON s.token_hash = encode(digest('dummy-access-' || t.username || '-' || t.n, 'sha256'), 'hex');

-- -----------------------------------------------------
-- audit_logs
-- -----------------------------------------------------
-- Login sukses
INSERT INTO audit_logs (user_id, action, entity, entity_id, ip_address, created_at)
SELECT u.id, 'LOGIN', 'users', u.id, m.ip::inet, now() - m.ago
FROM (VALUES
    ('superadmin',   '192.168.1.10', interval '1 hour'),
    ('admin.paiton', '192.168.1.21', interval '3 hours'),
    ('worker01',     '10.0.0.34',    interval '2 hours'),
    ('worker02',     '10.0.0.48',    interval '6 hours')
) AS m(username, ip, ago)
JOIN users u ON u.username = m.username;

-- Login gagal (user_id kosong karena username tidak dikenal)
INSERT INTO audit_logs (user_id, action, entity, after_data, ip_address, created_at) VALUES
    (NULL, 'LOGIN_FAILED', 'users', '{"username": "admin", "reason": "user_not_found"}',    '203.0.113.7',  now() - interval '5 hours'),
    (NULL, 'LOGIN_FAILED', 'users', '{"username": "root",  "reason": "user_not_found"}',    '203.0.113.7',  now() - interval '5 hours' + interval '1 minute');

-- Login gagal untuk worker08 (sampai terkunci)
INSERT INTO audit_logs (user_id, action, entity, entity_id, after_data, ip_address, created_at)
SELECT u.id, 'LOGIN_FAILED', 'users', u.id,
       jsonb_build_object('reason', 'wrong_password', 'attempt', g),
       '10.0.0.77'::inet,
       now() - interval '20 minutes' + (g || ' minutes')::interval
FROM users u, generate_series(1, 5) g
WHERE u.username = 'worker08';

-- CREATE user oleh superadmin
INSERT INTO audit_logs (user_id, action, entity, entity_id, before_data, after_data, ip_address, created_at)
SELECT sa.id, 'CREATE', 'users', u.id, NULL,
       jsonb_build_object('username', u.username, 'full_name', u.full_name),
       '192.168.1.10'::inet,
       now() - interval '10 days'
FROM users sa, users u
WHERE sa.username = 'superadmin'
  AND u.username IN ('admin.paiton', 'admin.suralaya', 'admin.cilegon');

-- UPDATE: contoh perubahan data user
INSERT INTO audit_logs (user_id, action, entity, entity_id, before_data, after_data, ip_address, created_at)
SELECT sa.id, 'UPDATE', 'users', u.id,
       '{"is_active": true}'::jsonb,
       '{"is_active": false}'::jsonb,
       '192.168.1.10'::inet,
       now() - interval '30 days'
FROM users sa, users u
WHERE sa.username = 'superadmin' AND u.username = 'worker09';

-- LOGOUT
INSERT INTO audit_logs (user_id, action, entity, entity_id, ip_address, created_at)
SELECT u.id, 'LOGOUT', 'users', u.id, '192.168.1.21'::inet, now() - interval '2 days'
FROM users u WHERE u.username = 'admin.paiton';

COMMIT;