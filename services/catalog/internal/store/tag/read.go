package tag

import (
	"context"
	"fmt"
	"strings"

	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

// ListTags lists tags by name; status "" means all, search matches the name.
func (s *Repo) ListTags(ctx context.Context, status, search string) ([]catalogapi.SeasonalTag, error) {
	q, args := tagSelect, []any{}
	if status != "" {
		args = append(args, status)
		q += fmt.Sprintf(" AND t.status = $%d", len(args))
	}
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+shared.LikeEscape(search)+"%")
		q += fmt.Sprintf(" AND t.name ILIKE $%d ESCAPE '\\'", len(args))
	}
	rows, err := s.pool.Query(ctx, q+" ORDER BY lower(t.name)", args...)
	if err != nil {
		return nil, err
	}
	return scanTags(rows)
}

// ListTagsInUse is the active tags mapped to at least one active product:
// the chips the storefront offers.
func (s *Repo) ListTagsInUse(ctx context.Context) ([]catalogapi.SeasonalTag, error) {
	rows, err := s.pool.Query(ctx, tagSelect+`
		AND t.status = 'active'
		AND EXISTS (SELECT 1 FROM product_seasonal_tags pt JOIN products p ON p.id = pt.product_id
		             WHERE pt.tag_id = t.id AND p.deleted_at IS NULL AND p.is_active = true)
		ORDER BY lower(t.name)`)
	if err != nil {
		return nil, err
	}
	return scanTags(rows)
}

func (s *Repo) GetTag(ctx context.Context, id string) (*catalogapi.SeasonalTag, error) {
	return scanTag(s.pool.QueryRow(ctx, tagSelect+" AND t.id::text = $1", id))
}

// TagsForProduct is a product's tags, by name.
func (s *Repo) TagsForProduct(ctx context.Context, productID string) ([]catalogapi.SeasonalTag, error) {
	rows, err := s.pool.Query(ctx, tagSelect+`
		AND t.id IN (SELECT tag_id FROM product_seasonal_tags WHERE product_id::text = $1)
		ORDER BY lower(t.name)`, productID)
	if err != nil {
		return nil, err
	}
	return scanTags(rows)
}

// TagIDsForSlugs finds tags by slug, creating any that do not exist yet — for
// writers that still send tag text (the catalogue import, older clients).
func (s *Repo) TagIDsForSlugs(ctx context.Context, names []string) ([]string, error) {
	var ids []string
	for _, n := range shared.UniqueStrings(names) {
		slug := catalogapi.TagSlug(n)
		if slug == "" {
			continue
		}
		var id string
		err := s.pool.QueryRow(ctx, `
			INSERT INTO seasonal_tags (name, slug) VALUES ($1, $2)
			ON CONFLICT (slug) WHERE deleted_at IS NULL DO UPDATE SET slug = EXCLUDED.slug
			RETURNING id`, displayTagName(n), slug).Scan(&id)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// RestrictionsForProducts is each product's restricted windows from its
// active tags.
func (s *Repo) RestrictionsForProducts(ctx context.Context, productIDs []string) (map[string][]catalogapi.TagRestriction, error) {
	out := map[string][]catalogapi.TagRestriction{}
	ids := shared.UniqueStrings(productIDs)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pt.product_id::text, t.id, t.name, t.restricted_from, t.restricted_to, t.repeat_yearly
		  FROM product_seasonal_tags pt
		  JOIN seasonal_tags t ON t.id = pt.tag_id
		 WHERE pt.product_id::text = ANY($1) AND t.deleted_at IS NULL AND t.status = 'active'
		   AND t.restricted AND t.restricted_from IS NOT NULL AND t.restricted_to IS NOT NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid string
		var r catalogapi.TagRestriction
		if err := rows.Scan(&pid, &r.TagID, &r.TagName, &r.From, &r.To, &r.RepeatYearly); err != nil {
			return nil, err
		}
		out[pid] = append(out[pid], r)
	}
	return out, rows.Err()
}
