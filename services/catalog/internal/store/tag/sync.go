package tag

import "context"

// syncOccasionTags writes a product's legacy occasion_tags from its tags'
// slugs, so anything still reading that column sees the same thing.
func (s *Repo) syncOccasionTags(ctx context.Context, productID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE products SET occasion_tags = COALESCE((
			SELECT array_agg(t.slug ORDER BY t.slug) FROM product_seasonal_tags pt
			  JOIN seasonal_tags t ON t.id = pt.tag_id AND t.deleted_at IS NULL
			 WHERE pt.product_id = products.id), '{}')
		 WHERE id::text = $1`, productID)
	return err
}

func (s *Repo) syncOccasionTagsForTag(ctx context.Context, tagID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE products SET occasion_tags = COALESCE((
			SELECT array_agg(t.slug ORDER BY t.slug) FROM product_seasonal_tags pt
			  JOIN seasonal_tags t ON t.id = pt.tag_id AND t.deleted_at IS NULL
			 WHERE pt.product_id = products.id), '{}')
		 WHERE id IN (SELECT product_id FROM product_seasonal_tags WHERE tag_id::text = $1)`, tagID)
	return err
}
