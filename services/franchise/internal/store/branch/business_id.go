package branch

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// EnsureBusinessID gives a branch the code its till signs in with.
//
// Derived from the branch code so it is recognisable on a printed card —
// "LDN-001-4827" is obviously the London Central shop — with four random digits
// so knowing one outlet's code tells you nothing about the next. Generated
// once and never changed: it is what the tills already have typed in.
//
// Retried on collision rather than trusted to be unique, because four
// digits over a growing estate will collide eventually and a failed branch
// creation is a poor way to find that out.
func (s *Repo) EnsureBusinessID(ctx context.Context, branchID, branchCode string) (string, error) {
	var existing *string
	if err := s.pool.QueryRow(ctx,
		`SELECT business_id FROM branches WHERE id = $1`, branchID).Scan(&existing); err != nil {
		return "", err
	}
	if existing != nil && *existing != "" {
		return *existing, nil
	}

	prefix := strings.ToUpper(strings.TrimSpace(branchCode))
	if prefix == "" {
		prefix = "LOC"
	}

	for attempt := 0; attempt < 12; attempt++ {
		n, err := rand.Int(rand.Reader, big.NewInt(9000))
		if err != nil {
			return "", err
		}
		candidate := fmt.Sprintf("%s-%04d", prefix, n.Int64()+1000)

		tag, err := s.pool.Exec(ctx,
			`UPDATE branches SET business_id = $2, updated_at = NOW()
			  WHERE id = $1 AND business_id IS NULL
			    AND NOT EXISTS (SELECT 1 FROM branches b2 WHERE b2.business_id = $2)
			    -- A central kitchen's Location ID is off limits too (KDS).
			    AND NOT EXISTS (SELECT 1 FROM central_kitchens ck
			                     WHERE ck.deleted_at IS NULL AND upper(btrim(ck.business_id)) = upper($2))`,
			branchID, candidate)
		if err != nil {
			return "", err
		}
		if tag.RowsAffected() == 1 {
			return candidate, nil
		}
		// Either somebody else just set it, or the code was taken. Re-read
		// before trying again: the first case is done, not a collision.
		if err := s.pool.QueryRow(ctx,
			`SELECT business_id FROM branches WHERE id = $1`, branchID).Scan(&existing); err != nil {
			return "", err
		}
		if existing != nil && *existing != "" {
			return *existing, nil
		}
	}
	return "", fmt.Errorf("could not allocate a business id for branch %s", branchID)
}

func (s *Repo) BranchTaxByID(ctx context.Context, branchID string) (taxName string, taxRatePct *float64, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT COALESCE(tax_name, 'Sales Tax'), tax_rate_pct FROM branches WHERE id = $1`,
		branchID).Scan(&taxName, &taxRatePct)
	if err != nil {
		return "", nil, err
	}
	return taxName, taxRatePct, nil
}

// BranchByBusinessID resolves the code typed on the POS login screen,
// matched exactly, case included.
// The name comes back too: the passcode screen should be able to say which
// shop is about to be signed in to. A till that has been set up with the
// wrong code should show it before somebody starts taking orders on it.
func (s *Repo) BranchByBusinessID(ctx context.Context, businessID string) (string, string, error) {
	var id, name string
	err := s.pool.QueryRow(ctx,
		`SELECT id, name FROM branches
		  WHERE btrim(business_id) = $1 AND status <> 'closed'`,
		strings.TrimSpace(businessID)).Scan(&id, &name)
	if err != nil {
		return "", "", err
	}
	return id, name, nil
}

/*
 * The code a till signs in with, minted when the outlet is created.
 *
 * It has to exist from the moment there is an outlet: a shop with no Business
 * ID cannot open a till at all, and until this generated one, every outlet
 * added through the console had to have one written into the database by hand
 * before its counter could be used.
 *
 * Built from the outlet's own code so it is recognisable on a printed card,
 * with four random digits so that two outlets sharing a code prefix — or one
 * created, deleted and created again — do not collide. The column is unique,
 * so a collision fails the insert rather than quietly giving two shops the
 * same sign-in.
 */
func newBusinessID(code string) string {
	prefix := strings.ToUpper(strings.TrimSpace(code))
	if prefix == "" {
		prefix = "LOC"
	}
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return prefix + "-0000"
	}
	return fmt.Sprintf("%s-%04d", prefix, n.Int64())
}

func coalesceBusinessID(id *string, code string) string {
	if id != nil && strings.TrimSpace(*id) != "" {
		return strings.TrimSpace(*id)
	}
	return newBusinessID(code)
}
