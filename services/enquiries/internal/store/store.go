package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/enquiries/enquiriesapi"
)

// ErrNotFound is returned when an enquiry or subscriber does not exist.
var ErrNotFound = errors.New("not found")

// Store is the enquiries service's data access.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const enquirySelect = `
	SELECT id::text, type, name, email, phone, message,
	       branch_id::text, details, status,
	       handled_by::text, handled_at, staff_note, created_at, updated_at
	FROM enquiries`

func scanEnquiry(row pgx.Row) (*enquiriesapi.Enquiry, error) {
	var e enquiriesapi.Enquiry
	var details []byte
	err := row.Scan(&e.ID, &e.Type, &e.Name, &e.Email, &e.Phone, &e.Message,
		&e.BranchID, &details, &e.Status,
		&e.HandledBy, &e.HandledAt, &e.StaffNote, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan enquiry: %w", err)
	}
	if len(details) > 0 {
		if err := json.Unmarshal(details, &e.Details); err != nil {
			return nil, fmt.Errorf("decode enquiry details: %w", err)
		}
	}
	return &e, nil
}

// Create inserts an enquiry and returns it as stored.
func (s *Store) Create(ctx context.Context, e enquiriesapi.Enquiry, sourceIP string) (*enquiriesapi.Enquiry, error) {
	details, err := json.Marshal(e.Details)
	if err != nil {
		return nil, fmt.Errorf("encode enquiry details: %w", err)
	}
	const q = `
		INSERT INTO enquiries (type, name, email, phone, message, branch_id, details, source_ip)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id::text, type, name, email, phone, message,
		          branch_id::text, details, status,
		          handled_by::text, handled_at, staff_note, created_at, updated_at`
	return scanEnquiry(s.pool.QueryRow(ctx, q,
		e.Type, e.Name, e.Email, e.Phone, e.Message, e.BranchID, details, nullIfEmpty(sourceIP)))
}

// Get returns one enquiry by id.
func (s *Store) Get(ctx context.Context, id string) (*enquiriesapi.Enquiry, error) {
	return scanEnquiry(s.pool.QueryRow(ctx, enquirySelect+" WHERE id = $1", id))
}

// List returns one page of the admin queue plus the total match count.
func (s *Store) List(ctx context.Context, f enquiriesapi.EnquiryFilter) ([]enquiriesapi.Enquiry, int, error) {
	f = f.Normalise()

	where := " WHERE 1=1"
	args := []interface{}{}
	add := func(cond string, val interface{}) {
		args = append(args, val)
		where += fmt.Sprintf(cond, len(args))
	}
	if f.Type != "" {
		add(" AND type = $%d", f.Type)
	}
	if f.Status != "" {
		add(" AND status = $%d", f.Status)
	}
	if f.BranchID != "" {
		add(" AND branch_id = $%d", f.BranchID)
	}
	if f.Search != "" {
		// One argument, matched against both columns, so the placeholder index
		// is substituted twice rather than consuming two arguments.
		args = append(args, "%"+f.Search+"%")
		where += fmt.Sprintf(" AND (name ILIKE $%[1]d OR email ILIKE $%[1]d)", len(args))
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM enquiries"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count enquiries: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	pageArgs := append(append([]interface{}{}, args...), f.Limit, f.Offset())
	q := enquirySelect + where +
		fmt.Sprintf(" ORDER BY created_at DESC, id LIMIT $%d OFFSET $%d", len(pageArgs)-1, len(pageArgs))

	rows, err := s.pool.Query(ctx, q, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query enquiries: %w", err)
	}
	defer rows.Close()

	var out []enquiriesapi.Enquiry
	for rows.Next() {
		e, err := scanEnquiry(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *e)
	}
	return out, total, rows.Err()
}

// UpdateStatus moves an enquiry through its lifecycle, recording who handled it.
func (s *Store) UpdateStatus(ctx context.Context, id, status, staffID string, note *string) (*enquiriesapi.Enquiry, error) {
	const q = `
		UPDATE enquiries
		SET status     = $2,
		    handled_by = $3,
		    handled_at = CASE WHEN $2 = 'new' THEN NULL ELSE now() END,
		    staff_note = COALESCE($4, staff_note),
		    updated_at = now()
		WHERE id = $1
		RETURNING id::text, type, name, email, phone, message,
				  branch_id::text, details, status,
		          handled_by::text, handled_at, staff_note, created_at, updated_at`
	return scanEnquiry(s.pool.QueryRow(ctx, q, id, status, nullIfEmpty(staffID), note))
}

// --- Newsletter --------------------------------------------------------------

// Subscribe records consent, reviving a previously unsubscribed address rather
// than creating a duplicate row.
func (s *Store) Subscribe(ctx context.Context, email, source string) (*enquiriesapi.NewsletterSubscriber, error) {
	const q = `
		INSERT INTO newsletter_subscribers (email, source)
		VALUES ($1, $2)
		ON CONFLICT (lower(email)) DO UPDATE
		SET unsubscribed_at = NULL,
		    consented_at    = now(),
		    source          = EXCLUDED.source
		RETURNING id::text, email, source, consented_at, unsubscribed_at`
	var n enquiriesapi.NewsletterSubscriber
	if err := s.pool.QueryRow(ctx, q, email, source).
		Scan(&n.ID, &n.Email, &n.Source, &n.ConsentedAt, &n.UnsubscribedAt); err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", email, err)
	}
	return &n, nil
}

// Unsubscribe stamps the withdrawal. Unknown addresses are treated as success
// so the endpoint cannot be used to probe who is subscribed.
func (s *Store) Unsubscribe(ctx context.Context, email string) error {
	const q = `UPDATE newsletter_subscribers SET unsubscribed_at = now()
	           WHERE lower(email) = lower($1) AND unsubscribed_at IS NULL`
	if _, err := s.pool.Exec(ctx, q, email); err != nil {
		return fmt.Errorf("unsubscribe %s: %w", email, err)
	}
	return nil
}

func nullIfEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
