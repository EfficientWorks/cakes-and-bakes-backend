package domain

import (
	"context"
	"errors"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store"
)

// RecordCustomerPhoto notes a photo a customer just uploaded (Custom Orders
// form), so it can later be attached to an order.
func (s *Service) RecordCustomerPhoto(ctx context.Context, id, url, contentType string, byteSize int, sourceIP string) error {
	if err := s.store.RecordCustomerPhoto(ctx, id, url, contentType, byteSize, sourceIP); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// ResolveCustomerPhoto implements catalogapi.CustomerPhotos.
func (s *Service) ResolveCustomerPhoto(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", catalogapi.ErrCustomerPhotoNotFound
	}
	url, err := s.store.CustomerPhotoURL(ctx, ref)
	if errors.Is(err, store.ErrNotFound) {
		return "", catalogapi.ErrCustomerPhotoNotFound
	}
	if err != nil {
		return "", err
	}
	return url, nil
}
