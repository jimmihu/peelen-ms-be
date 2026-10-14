package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
	"peelen-ms-be/internal/scoring"
)

const zeroUUID = "00000000-0000-0000-0000-000000000000"

func (r *ScoringRepositoryImpl) VendorSiteLinked(ctx context.Context, vendorID, siteID string) (bool, error) {
	var linked bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM vendor_sites WHERE vendor_id = $1 AND site_id = $2)`,
		vendorID, siteID).Scan(&linked)
	return linked, err
}

// CreateRuleVersion supersedes the active version (if any) and inserts the next one.
func (r *ScoringRepositoryImpl) CreateRuleVersion(ctx context.Context, in entity.ScoreRuleInput, createdBy string) (*entity.ScoreRule, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize versioning per (kind, vendor, site).
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text || $2::text || coalesce($3::text, '')))`,
		in.Kind, in.VendorID, in.SiteID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE score_rules SET superseded_at = now()
		WHERE kind = $1 AND vendor_id = $2 AND coalesce(site_id, $4::uuid) = coalesce($3::uuid, $4::uuid)
			AND superseded_at IS NULL`, in.Kind, in.VendorID, in.SiteID, zeroUUID); err != nil {
		return nil, translateOperationalDBError(err)
	}
	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO score_rules (kind, vendor_id, site_id, period_type, version, created_by)
		SELECT $1, $2, $3, $4, coalesce(max(version), 0) + 1, $5
		FROM score_rules
		WHERE kind = $1 AND vendor_id = $2 AND coalesce(site_id, $6::uuid) = coalesce($3::uuid, $6::uuid)
		RETURNING id::text`, in.Kind, in.VendorID, in.SiteID, in.PeriodType, createdBy, zeroUUID).Scan(&id); err != nil {
		return nil, translateOperationalDBError(err)
	}
	for _, item := range in.Items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO score_rule_items (rule_id, code, weight, target) VALUES ($1, $2, $3, $4)`,
			id, item.Code, item.Weight, item.Target); err != nil {
			return nil, translateOperationalDBError(err)
		}
	}
	for _, level := range in.Levels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO score_rule_levels (rule_id, level, min_score, min_dimension, consecutive_periods)
			VALUES ($1, $2, $3, $4, $5)`, id, level.Level, level.MinScore, level.MinDimension, level.ConsecutivePeriods); err != nil {
			return nil, translateOperationalDBError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetRule(ctx, id)
}

const ruleColumns = `r.id::text, r.kind, r.vendor_id::text, r.site_id::text, r.period_type, r.version,
	r.created_by::text, r.created_at, r.superseded_at`

func scanRule(row interface{ Scan(...any) error }) (entity.ScoreRule, error) {
	var rule entity.ScoreRule
	var siteID, createdBy sql.NullString
	var superseded sql.NullTime
	err := row.Scan(&rule.ID, &rule.Kind, &rule.VendorID, &siteID, &rule.PeriodType, &rule.Version,
		&createdBy, &rule.CreatedAt, &superseded)
	rule.SiteID = nullStringPtr(siteID)
	rule.CreatedBy = nullStringPtr(createdBy)
	rule.SupersededAt = nullTimePtr(superseded)
	return rule, err
}

func (r *ScoringRepositoryImpl) loadRuleParts(ctx context.Context, rule *entity.ScoreRule) error {
	rows, err := r.db.QueryContext(ctx, `SELECT code, weight, target FROM score_rule_items WHERE rule_id = $1 ORDER BY weight DESC, code`, rule.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item entity.RuleItem
		var target sql.NullFloat64
		if err := rows.Scan(&item.Code, &item.Weight, &target); err != nil {
			return err
		}
		if target.Valid {
			item.Target = &target.Float64
		}
		rule.Items = append(rule.Items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	levels, err := r.db.QueryContext(ctx, `SELECT level, min_score, min_dimension, consecutive_periods
		FROM score_rule_levels WHERE rule_id = $1 ORDER BY level`, rule.ID)
	if err != nil {
		return err
	}
	defer levels.Close()
	for levels.Next() {
		var l scoring.LevelRule
		if err := levels.Scan(&l.Level, &l.MinScore, &l.MinDimension, &l.ConsecutivePeriods); err != nil {
			return err
		}
		rule.Levels = append(rule.Levels, l)
	}
	return levels.Err()
}

func (r *ScoringRepositoryImpl) GetRule(ctx context.Context, id string) (*entity.ScoreRule, error) {
	rule, err := scanRule(r.db.QueryRowContext(ctx, `SELECT `+ruleColumns+` FROM score_rules r WHERE r.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := r.loadRuleParts(ctx, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *ScoringRepositoryImpl) ListRules(ctx context.Context, f entity.RuleFilter) ([]entity.ScoreRule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+ruleColumns+` FROM score_rules r
		WHERE ($1 = '' OR r.kind = $1)
			AND ($2 = '' OR r.vendor_id::text = $2)
			AND ($3::uuid IS NULL OR r.site_id = $3::uuid)
			AND (NOT $4 OR r.superseded_at IS NULL)
			AND ($5 OR r.vendor_id = ANY($6::uuid[]))
		ORDER BY r.vendor_id, r.site_id NULLS FIRST, r.kind, r.version DESC`,
		f.Kind, f.VendorID, f.SiteID, f.ActiveOnly, f.AllVendors, pq.Array(f.VendorIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []entity.ScoreRule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range rules {
		if err := r.loadRuleParts(ctx, &rules[i]); err != nil {
			return nil, err
		}
	}
	return rules, nil
}

func (r *ScoringRepositoryImpl) DeactivateRule(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE score_rules SET superseded_at = now() WHERE id = $1 AND superseded_at IS NULL`, id)
	if err != nil {
		return err
	}
	return workflowRequireAffected(result)
}

func (r *ScoringRepositoryImpl) VendorIDsForSites(ctx context.Context, siteIDs []string) ([]string, error) {
	var ids pq.StringArray
	err := r.db.QueryRowContext(ctx, `SELECT coalesce(array_agg(DISTINCT vendor_id::text), '{}') FROM vendor_sites WHERE site_id = ANY($1::uuid[])`,
		pq.Array(siteIDs)).Scan(&ids)
	return []string(ids), err
}
