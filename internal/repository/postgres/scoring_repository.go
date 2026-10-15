package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

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

func (r *ScoringRepositoryImpl) RuleKeys(ctx context.Context) ([]entity.RuleKey, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT kind, vendor_id::text, site_id::text FROM score_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []entity.RuleKey
	for rows.Next() {
		var key entity.RuleKey
		var siteID sql.NullString
		if err := rows.Scan(&key.Kind, &key.VendorID, &siteID); err != nil {
			return nil, err
		}
		key.SiteID = nullStringPtr(siteID)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (r *ScoringRepositoryImpl) RuleVersions(ctx context.Context, key entity.RuleKey) ([]entity.ScoreRule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+ruleColumns+` FROM score_rules r
		WHERE r.kind = $1 AND r.vendor_id = $2 AND coalesce(r.site_id, $4::uuid) = coalesce($3::uuid, $4::uuid)
		ORDER BY r.version`, key.Kind, key.VendorID, key.SiteID, zeroUUID)
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

// OverrideSitesAt lists sites with a site override of kind active at the instant.
func (r *ScoringRepositoryImpl) OverrideSitesAt(ctx context.Context, kind, vendorID string, at time.Time) ([]string, error) {
	var ids pq.StringArray
	err := r.db.QueryRowContext(ctx, `SELECT coalesce(array_agg(DISTINCT site_id::text), '{}') FROM score_rules
		WHERE kind = $1 AND vendor_id = $2 AND site_id IS NOT NULL
			AND created_at <= $3 AND (superseded_at IS NULL OR superseded_at > $3)`, kind, vendorID, at).Scan(&ids)
	return []string(ids), err
}

func (r *ScoringRepositoryImpl) VendorSites(ctx context.Context, vendorID string) ([]string, error) {
	var ids pq.StringArray
	err := r.db.QueryRowContext(ctx, `SELECT coalesce(array_agg(site_id::text ORDER BY site_id), '{}') FROM vendor_sites WHERE vendor_id = $1`,
		vendorID).Scan(&ids)
	return []string(ids), err
}

func (r *ScoringRepositoryImpl) ResultExists(ctx context.Context, kind, scope, vendorID string, siteID *string, periodType string, start time.Time) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM score_period_results
		WHERE kind = $1 AND scope_type = $2 AND vendor_id = $3 AND coalesce(site_id, $7::uuid) = coalesce($4::uuid, $7::uuid)
			AND worker_id IS NULL AND period_type = $5 AND period_start = $6)`,
		kind, scope, vendorID, siteID, periodType, start, zeroUUID).Scan(&exists)
	return exists, err
}

func (r *ScoringRepositoryImpl) SaveResult(ctx context.Context, res entity.ScoreResult) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var score, compliant, level any
	if res.Status == scoring.StatusOK {
		score = res.Score
		if res.Kind == entity.RuleKindSLA {
			compliant = res.Compliant
		} else {
			level = res.Level
		}
	} else if res.Kind == entity.RuleKindMaturity {
		level = res.Level
	}
	var id string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO score_period_results (kind, rule_id, scope_type, vendor_id, site_id, worker_id, period_type,
			period_start, period_end, status, score, compliant, level, task_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT DO NOTHING
		RETURNING id::text`, res.Kind, res.RuleID, res.ScopeType, res.VendorID, res.SiteID, res.WorkerID, res.PeriodType,
		res.PeriodStart, res.PeriodEnd, res.Status, score, compliant, level, res.TaskCount).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already frozen
	}
	if err != nil {
		return translateOperationalDBError(err)
	}
	for _, item := range res.Items {
		var target, met any
		if res.Kind == entity.RuleKindSLA {
			target, met = item.Target, item.Met
		}
		var actual, itemScore any
		if !item.NA {
			actual, itemScore = item.Actual, item.Score
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO score_period_item_results (result_id, code, actual, target, score, weight, met, is_na)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, item.Code, actual, target, itemScore, item.Weight, met, item.NA); err != nil {
			return translateOperationalDBError(err)
		}
	}
	return tx.Commit()
}

func (r *ScoringRepositoryImpl) ListResults(ctx context.Context, f entity.ResultFilter) ([]entity.ScoreResult, int, error) {
	where := `kind = $1 AND scope_type = $2 AND period_start >= $3 AND period_start < $4
		AND ($5 = '' OR vendor_id::text = $5) AND ($6 = '' OR site_id::text = $6) AND ($7 = '' OR worker_id::text = $7)
		AND ($8 OR vendor_id = ANY($9::uuid[]) OR site_id = ANY($10::uuid[]))
		AND ($11 = '' OR worker_id::text = $11)`
	args := []any{f.Kind, f.Scope, f.From, f.To, f.VendorID, f.SiteID, f.WorkerID,
		f.AllVendors, pq.Array(f.VendorIDs), pq.Array(f.SiteIDs), f.OnlyWorkerID}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM score_period_results WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, kind, rule_id::text, scope_type, vendor_id::text, site_id::text, worker_id::text, period_type,
			period_start, period_end, status, score, compliant, level, task_count
		FROM score_period_results WHERE `+where+`
		ORDER BY period_start DESC, vendor_id, site_id NULLS FIRST, worker_id NULLS FIRST
		LIMIT $12 OFFSET $13`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var results []entity.ScoreResult
	for rows.Next() {
		var res entity.ScoreResult
		var siteID, workerID sql.NullString
		var score sql.NullFloat64
		var compliant sql.NullBool
		var level sql.NullInt64
		if err := rows.Scan(&res.ID, &res.Kind, &res.RuleID, &res.ScopeType, &res.VendorID, &siteID, &workerID,
			&res.PeriodType, &res.PeriodStart, &res.PeriodEnd, &res.Status, &score, &compliant, &level, &res.TaskCount); err != nil {
			return nil, 0, err
		}
		res.SiteID, res.WorkerID = nullStringPtr(siteID), nullStringPtr(workerID)
		res.Score, res.Compliant, res.Level = score.Float64, compliant.Bool, int(level.Int64)
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range results {
		items, err := r.db.QueryContext(ctx, `SELECT code, coalesce(actual, 0), coalesce(target, 0), coalesce(score, 0), weight,
			coalesce(met, false), is_na FROM score_period_item_results WHERE result_id = $1 ORDER BY weight DESC, code`, results[i].ID)
		if err != nil {
			return nil, 0, err
		}
		for items.Next() {
			var it scoring.ItemResult
			if err := items.Scan(&it.Code, &it.Actual, &it.Target, &it.Score, &it.Weight, &it.Met, &it.NA); err != nil {
				items.Close()
				return nil, 0, err
			}
			results[i].Items = append(results[i].Items, it)
		}
		items.Close()
	}
	return results, total, nil
}

// scoringJobLockKey identifies the scoring job's advisory lock.
const scoringJobLockKey = 7_420_015

// WithJobLock runs fn only if this process wins the advisory lock; the lock is
// held on a dedicated connection for fn's duration.
func (r *ScoringRepositoryImpl) WithJobLock(ctx context.Context, fn func(context.Context) error) (bool, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, scoringJobLockKey).Scan(&locked); err != nil {
		return false, err
	}
	if !locked {
		return false, nil
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, scoringJobLockKey)
	return true, fn(ctx)
}
