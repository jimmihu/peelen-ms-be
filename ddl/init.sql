-- =====================================================
-- Skema PostgreSQL: users, roles, scopes, auth, audit
-- Jalankan di database tujuan (mis. CREATE DATABASE belajar; \c belajar)
-- gen_random_uuid() bawaan PostgreSQL 13+
-- =====================================================

BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- -----------------------------------------------------
-- Tabel master: sites & vendors (dibuat lebih dulu
-- karena direferensikan oleh user_scopes)
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS sites (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code       varchar(20)  NOT NULL UNIQUE,
    name       varchar(150) NOT NULL,   -- mis. PLTU Paiton
    site_type  varchar(30),             -- mis. PLTU, pabrik, gedung
    -- Zona waktu IANA lokasi: Asia/Jakarta (WIB), Asia/Makassar (WITA), Asia/Jayapura (WIT).
    -- Jam shift, "hari ini", batas upload, dan jam foto tanpa offset dihitung di zona ini.
    timezone   varchar(64) NOT NULL DEFAULT 'Asia/Jakarta',
    status     varchar(10) NOT NULL DEFAULT 'ACTIVE'
               CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS vendors (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code       varchar(20)  NOT NULL UNIQUE,
    name       varchar(150) NOT NULL,
    status     varchar(10) NOT NULL DEFAULT 'ACTIVE'
               CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- -----------------------------------------------------
-- users
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username             varchar(30)  NOT NULL UNIQUE,   -- login, lowercase, tidak bisa diubah
    password_hash        text         NOT NULL,
    full_name            varchar(150),
    email                varchar(150) UNIQUE,            -- opsional, untuk kontak/notifikasi
    phone                varchar(30),
    must_change_password boolean      NOT NULL DEFAULT true,
    failed_login_count   int          NOT NULL DEFAULT 0,
    locked_until         timestamptz,
    is_active            boolean      NOT NULL DEFAULT true,
    last_login_at        timestamptz,
    created_at           timestamptz  NOT NULL DEFAULT now(),
    updated_at           timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT users_username_lowercase CHECK (username = lower(username))
);

ALTER TABLE sites
    ADD COLUMN IF NOT EXISTS created_by uuid REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE sites
    ADD COLUMN IF NOT EXISTS status varchar(10) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE'));
ALTER TABLE sites
    ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE sites
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE vendors
    ADD COLUMN IF NOT EXISTS created_by uuid REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE vendors
    ADD COLUMN IF NOT EXISTS status varchar(10) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE'));
ALTER TABLE vendors
    ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE vendors
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

-- Trigger: username tidak bisa diubah
CREATE OR REPLACE FUNCTION prevent_username_change()
RETURNS trigger AS $$
BEGIN
    IF NEW.username IS DISTINCT FROM OLD.username THEN
        RAISE EXCEPTION 'username tidak boleh diubah';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_users_username_immutable ON users;
CREATE TRIGGER trg_users_username_immutable
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION prevent_username_change();

-- Trigger: updated_at otomatis
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
CREATE TRIGGER trg_users_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------
-- roles
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS roles (
    id    smallint PRIMARY KEY,
    code  varchar(30) NOT NULL UNIQUE,  -- SUPER_ADMIN | ADMIN | ADMIN_WORKER | WORKER
    name  varchar(50) NOT NULL,

    CONSTRAINT roles_code_valid
        CHECK (code IN ('SUPER_ADMIN', 'ADMIN', 'ADMIN_WORKER', 'WORKER'))
);

INSERT INTO roles (id, code, name) VALUES
    (1, 'SUPER_ADMIN',  'Super Admin'),
    (2, 'ADMIN',        'Admin'),
    (3, 'ADMIN_WORKER', 'Admin Worker'),
    (4, 'WORKER',       'Worker')
ON CONFLICT (id) DO NOTHING;

-- -----------------------------------------------------
-- user_roles
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS user_roles (
    user_id     uuid     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id     smallint NOT NULL REFERENCES roles(id),
    assigned_at timestamptz NOT NULL DEFAULT now(),
    assigned_by uuid     REFERENCES users(id) ON DELETE SET NULL,

    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_role_id ON user_roles(role_id);

-- -----------------------------------------------------
-- user_scopes
-- SUPER_ADMIN tidak butuh baris scope (akses semua)
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS user_scopes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    site_id    uuid REFERENCES sites(id)   ON DELETE CASCADE,   -- scope ADMIN
    vendor_id  uuid REFERENCES vendors(id) ON DELETE CASCADE,   -- scope ADMIN_WORKER / WORKER
    created_at timestamptz NOT NULL DEFAULT now(),

    -- minimal salah satu terisi
    CONSTRAINT user_scopes_site_or_vendor
        CHECK (site_id IS NOT NULL OR vendor_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_user_scopes_user_id   ON user_scopes(user_id);
CREATE INDEX IF NOT EXISTS idx_user_scopes_site_id   ON user_scopes(site_id)   WHERE site_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_user_scopes_vendor_id ON user_scopes(vendor_id) WHERE vendor_id IS NOT NULL;

-- Cegah scope ganda yang sama untuk satu user
CREATE UNIQUE INDEX IF NOT EXISTS uq_user_scopes_site
    ON user_scopes(user_id, site_id) WHERE vendor_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_user_scopes_vendor
    ON user_scopes(user_id, vendor_id) WHERE site_id IS NULL;

-- -----------------------------------------------------
-- sessions
-- Satu baris per sesi login. Simpan HASH token, bukan token mentah.
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS sessions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash     text NOT NULL UNIQUE,   -- UNIQUE sudah otomatis membuat index
    expires_at     timestamptz NOT NULL,
    revoked_at     timestamptz,            -- NULL = masih aktif
    last_active_at timestamptz,
    user_agent     text,
    ip_address     inet,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id    ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- -----------------------------------------------------
-- refresh_tokens
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    user_agent text,
    ip_address inet,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id    ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);

-- -----------------------------------------------------
-- audit_logs
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS audit_logs (
    id          bigserial PRIMARY KEY,
    user_id     uuid REFERENCES users(id) ON DELETE SET NULL,  -- kosong untuk login gagal
    action      varchar(30) NOT NULL,   -- LOGIN | LOGOUT | LOGIN_FAILED | CREATE | UPDATE | DELETE
    entity      varchar(50),
    entity_id   uuid,
    before_data jsonb,
    after_data  jsonb,
    ip_address  inet,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT audit_logs_action_valid
        CHECK (action IN ('LOGIN', 'LOGOUT', 'LOGIN_FAILED', 'CREATE', 'UPDATE', 'DELETE'))
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_user_created ON audit_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_entity     ON audit_logs(entity, entity_id);

-- -----------------------------------------------------
-- Relasi site/vendor
-- Many-to-many supaya satu vendor dapat melayani beberapa site.
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS vendor_sites (
    vendor_id  uuid NOT NULL REFERENCES vendors(id) ON DELETE CASCADE,
    site_id    uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (vendor_id, site_id)
);

CREATE INDEX IF NOT EXISTS idx_vendor_sites_site_id ON vendor_sites(site_id);

-- -----------------------------------------------------
-- Master operasional
-- File/image columns store object-storage keys, not binary data.
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS inventories (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id        uuid NOT NULL REFERENCES vendors(id) ON DELETE RESTRICT,
    item             varchar(150) NOT NULL,
    jenis_item       varchar(100),
    qty              integer NOT NULL DEFAULT 0 CHECK (qty >= 0),
    img              text,
    status_condition varchar(20) NOT NULL DEFAULT 'GOOD'
                     CHECK (status_condition IN ('GOOD', 'DAMAGED', 'MAINTENANCE', 'LOST')),
    status           varchar(10) NOT NULL DEFAULT 'ACTIVE'
                     CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_by       uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, vendor_id)
);

CREATE INDEX IF NOT EXISTS idx_inventories_vendor_status
    ON inventories(vendor_id, status);

CREATE TABLE IF NOT EXISTS policies (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id  uuid NOT NULL REFERENCES vendors(id) ON DELETE RESTRICT,
    name       varchar(150) NOT NULL,
    file_url   text NOT NULL, -- object-storage key for the PDF
    status     varchar(10) NOT NULL DEFAULT 'ACTIVE'
               CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_policies_vendor_status
    ON policies(vendor_id, status);

CREATE TABLE IF NOT EXISTS pipelines (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id   uuid NOT NULL REFERENCES vendors(id) ON DELETE RESTRICT,
    site_id     uuid NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    name        varchar(150) NOT NULL,
    description text,
    status      varchar(10) NOT NULL DEFAULT 'ACTIVE'
                CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_pipelines_site_id ON pipelines(site_id);

CREATE INDEX IF NOT EXISTS idx_pipelines_vendor_status
    ON pipelines(vendor_id, status);

CREATE TABLE IF NOT EXISTS items (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        varchar(150) NOT NULL,
    description text,
    status      varchar(10) NOT NULL DEFAULT 'ACTIVE'
                CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_items_status ON items(status);

CREATE TABLE IF NOT EXISTS pipeline_items (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id uuid NOT NULL REFERENCES pipelines(id) ON DELETE RESTRICT,
    item_id     uuid NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
    sort_order  integer NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    status      varchar(10) NOT NULL DEFAULT 'ACTIVE'
                CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (pipeline_id, item_id),
    UNIQUE (id, pipeline_id)
);

CREATE INDEX IF NOT EXISTS idx_pipeline_items_pipeline_status
    ON pipeline_items(pipeline_id, status, sort_order);

CREATE TABLE IF NOT EXISTS shifts (
    shift      varchar(10) PRIMARY KEY CHECK (shift IN ('DAY', 'EVENING', 'NIGHT')),
    start_time time NOT NULL,
    end_time   time NOT NULL
);

INSERT INTO shifts (shift, start_time, end_time) VALUES
    ('DAY',     '08:00', '16:00'),
    ('EVENING', '16:00', '00:00'),
    ('NIGHT',   '00:00', '08:00')
ON CONFLICT (shift) DO UPDATE
SET start_time = EXCLUDED.start_time, end_time = EXCLUDED.end_time;

-- -----------------------------------------------------
-- Worker assignments and daily task reporting
-- Each assignment targets a pipeline item; reports are per assignment, shift and work date.
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS pipeline_item_assignments (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_item_id  uuid NOT NULL REFERENCES pipeline_items(id) ON DELETE RESTRICT,
    user_id           uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    is_active         boolean NOT NULL DEFAULT true,
    assigned_by       uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (pipeline_item_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_pipeline_assignments_user_active
    ON pipeline_item_assignments(user_id, is_active);
CREATE INDEX IF NOT EXISTS idx_pipeline_assignments_item_active
    ON pipeline_item_assignments(pipeline_item_id, is_active);

CREATE TABLE IF NOT EXISTS pipeline_reports (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_item_assignment_id uuid NOT NULL REFERENCES pipeline_item_assignments(id) ON DELETE RESTRICT,
    shift                varchar(10) NOT NULL REFERENCES shifts(shift) ON UPDATE CASCADE ON DELETE RESTRICT,
    report_date          date NOT NULL,
    status               varchar(20) NOT NULL DEFAULT 'IN_PROGRESS'
                         CHECK (status IN ('IN_PROGRESS', 'COMPLETED')),
    -- SUBMITTED = worker mengirim/mengubah report setelah shift selesai (upload susulan)
    status_revisi        varchar(20) NOT NULL DEFAULT 'NONE'
                         CHECK (status_revisi IN ('NONE', 'SUBMITTED')),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (pipeline_item_assignment_id, shift, report_date)
);

CREATE INDEX IF NOT EXISTS idx_pipeline_reports_date_status
    ON pipeline_reports(report_date, status, status_revisi);
CREATE INDEX IF NOT EXISTS idx_pipeline_reports_assignment_date
    ON pipeline_reports(pipeline_item_assignment_id, report_date DESC);

CREATE TABLE IF NOT EXISTS pipeline_report_details (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_report_id uuid NOT NULL UNIQUE REFERENCES pipeline_reports(id) ON DELETE RESTRICT,
    evidence_before    text, -- object-storage key
    evidence_after     text, -- object-storage key
    open_date          timestamptz,
    closed_date        timestamptz,
    note_category      varchar(30)
                       CHECK (note_category IS NULL OR note_category IN ('NEAR_MISS', 'UNSAFE_ACTION', 'UNSAFE_CONDITION')),
    notes              text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CHECK (closed_date IS NULL OR open_date IS NULL OR closed_date >= open_date)
);

CREATE TABLE IF NOT EXISTS pipeline_report_history (
    id                  bigserial PRIMARY KEY,
    pipeline_report_id  uuid NOT NULL REFERENCES pipeline_reports(id) ON DELETE RESTRICT,
    actor_id            uuid REFERENCES users(id) ON DELETE SET NULL,
    previous_status     varchar(20),
    new_status          varchar(20) NOT NULL
                        CHECK (new_status IN ('IN_PROGRESS', 'COMPLETED')),
    previous_revision   varchar(20),
    new_revision        varchar(20) NOT NULL
                        CHECK (new_revision IN ('NONE', 'SUBMITTED')),
    reason              text,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_pipeline_report_history_report_created
    ON pipeline_report_history(pipeline_report_id, created_at);

-- -----------------------------------------------------
-- Inventory reports
-- REQUEST reports may refer to no existing inventory; DAMAGED reports must.
-- -----------------------------------------------------
CREATE TABLE IF NOT EXISTS inventory_reports (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id    uuid NOT NULL REFERENCES vendors(id) ON DELETE RESTRICT,
    inventory_id uuid,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    report_type  varchar(10) NOT NULL CHECK (report_type IN ('REQUEST', 'DAMAGED')),
    evidence     text, -- MinIO object key for an optional photo (JPEG, PNG, or WebP)
    description  text NOT NULL,
    status       varchar(20) NOT NULL DEFAULT 'SUBMITTED'
                 CHECK (status IN ('SUBMITTED', 'IN_REVIEW', 'APPROVED', 'REJECTED', 'RESOLVED')),
    review_reason text,
    reviewed_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (inventory_id, vendor_id)
        REFERENCES inventories(id, vendor_id) ON DELETE RESTRICT,
    CHECK (report_type <> 'DAMAGED' OR inventory_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_inventory_reports_vendor_status
    ON inventory_reports(vendor_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_reports_user_created
    ON inventory_reports(user_id, created_at DESC);

-- -----------------------------------------------------
-- SLA & maturity scoring
-- -----------------------------------------------------
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

-- Keep updated_at consistent across mutable master and report tables.
DROP TRIGGER IF EXISTS trg_sites_updated_at ON sites;
CREATE TRIGGER trg_sites_updated_at BEFORE UPDATE ON sites
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_vendors_updated_at ON vendors;
CREATE TRIGGER trg_vendors_updated_at BEFORE UPDATE ON vendors
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_inventories_updated_at ON inventories;
CREATE TRIGGER trg_inventories_updated_at BEFORE UPDATE ON inventories
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_policies_updated_at ON policies;
CREATE TRIGGER trg_policies_updated_at BEFORE UPDATE ON policies
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_pipelines_updated_at ON pipelines;
CREATE TRIGGER trg_pipelines_updated_at BEFORE UPDATE ON pipelines
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_items_updated_at ON items;
CREATE TRIGGER trg_items_updated_at BEFORE UPDATE ON items
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_pipeline_items_updated_at ON pipeline_items;
CREATE TRIGGER trg_pipeline_items_updated_at BEFORE UPDATE ON pipeline_items
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_pipeline_item_assignments_updated_at ON pipeline_item_assignments;
CREATE TRIGGER trg_pipeline_item_assignments_updated_at BEFORE UPDATE ON pipeline_item_assignments
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_pipeline_reports_updated_at ON pipeline_reports;
CREATE TRIGGER trg_pipeline_reports_updated_at BEFORE UPDATE ON pipeline_reports
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_pipeline_report_details_updated_at ON pipeline_report_details;
CREATE TRIGGER trg_pipeline_report_details_updated_at BEFORE UPDATE ON pipeline_report_details
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_inventory_reports_updated_at ON inventory_reports;
CREATE TRIGGER trg_inventory_reports_updated_at BEFORE UPDATE ON inventory_reports
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;