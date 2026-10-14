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
