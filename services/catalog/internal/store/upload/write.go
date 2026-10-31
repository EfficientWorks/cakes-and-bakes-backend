package upload

import (
	"context"
	"fmt"

	"bakehouse/pkg/tenant"
)

// RecordCustomerPhoto notes one stored customer photo.
func (s *Repo) RecordCustomerPhoto(ctx context.Context, id, url, contentType string, byteSize int, sourceIP string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO customer_photo_uploads (id, url, content_type, byte_size, business_id, source_ip)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
		ON CONFLICT (id) DO NOTHING`,
		id, url, contentType, byteSize, tenant.BusinessParam(ctx), sourceIP)
	if err != nil {
		return fmt.Errorf("record customer photo: %w", err)
	}
	return nil
}
