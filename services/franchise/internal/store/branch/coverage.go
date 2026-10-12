package branch

import (
	"context"

	"bakehouse/services/franchise/franchiseapi"
)

func (s *Repo) CreateZoneCoverageRequest(ctx context.Context, email, postcode string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO zone_coverage_requests (email, postcode) VALUES ($1,$2)`, email, postcode)
	return err
}

func (s *Repo) ListGovernanceRules(ctx context.Context) ([]franchiseapi.GovernanceRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT field_key, level, description FROM governance_rules ORDER BY level, field_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []franchiseapi.GovernanceRule
	for rows.Next() {
		var g franchiseapi.GovernanceRule
		if err := rows.Scan(&g.FieldKey, &g.Level, &g.Description); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
