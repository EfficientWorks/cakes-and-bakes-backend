package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store"
)

/*
 * Seasonal tags: created once, mapped to products, filtered on by id.
 * See catalogapi/seasonal_tags.go for what restricted dates mean.
 */

// TagInput is a tag as admin saves it. Times are store-local wall clock.
type TagInput struct {
	Name           string
	Description    *string
	Status         string
	Restricted     bool
	RestrictedFrom *time.Time
	RestrictedTo   *time.Time
	RepeatYearly   bool
}

func (s *Service) ListTags(ctx context.Context, status, search string) ([]catalogapi.SeasonalTag, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "all" {
		status = ""
	}
	if status != "" && status != catalogapi.TagActive && status != catalogapi.TagInactive {
		return nil, apperr.Validation("status must be active or inactive.")
	}
	tags, err := s.store.ListTags(ctx, status, search)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return tags, nil
}

// StorefrontTags are the active tags that have products to show.
func (s *Service) StorefrontTags(ctx context.Context) ([]catalogapi.SeasonalTag, error) {
	tags, err := s.store.ListTagsInUse(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return tags, nil
}

func (s *Service) GetTag(ctx context.Context, id string) (*catalogapi.SeasonalTag, error) {
	t, err := s.store.GetTag(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Tag not found.")
	}
	return t, nil
}

func (s *Service) CreateTag(ctx context.Context, claims *auth.StaffClaims, in TagInput) (*catalogapi.SeasonalTag, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return nil, apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	t, err := tagFromInput(in)
	if err != nil {
		return nil, err
	}
	out, err := s.store.CreateTag(ctx, t)
	if err != nil {
		return nil, tagWriteError(err)
	}
	s.writeAudit(ctx, claims, "tag.create", "seasonal_tag", &out.ID, nil, nil, out)
	return out, nil
}

func (s *Service) UpdateTag(ctx context.Context, claims *auth.StaffClaims, id string, in TagInput) (*catalogapi.SeasonalTag, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return nil, apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	before, err := s.store.GetTag(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Tag not found.")
	}
	t, err := tagFromInput(in)
	if err != nil {
		return nil, err
	}
	t.ID = before.ID
	out, err := s.store.UpdateTag(ctx, t)
	if err != nil {
		return nil, tagWriteError(err)
	}
	s.writeAudit(ctx, claims, "tag.update", "seasonal_tag", &out.ID, nil, before, out)
	return out, nil
}

func (s *Service) DeleteTag(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	if err := s.store.DeleteTag(ctx, id); err != nil {
		return notFoundOrInternal(err, "Tag not found.")
	}
	s.writeAudit(ctx, claims, "tag.delete", "seasonal_tag", &id, nil, nil, nil)
	return nil
}

func tagFromInput(in TagInput) (catalogapi.SeasonalTag, error) {
	t := catalogapi.SeasonalTag{
		Name: strings.TrimSpace(in.Name), Description: trimmedPtr(in.Description),
		Status: strings.ToLower(strings.TrimSpace(in.Status)), RepeatYearly: in.RepeatYearly,
	}
	if t.Name == "" {
		return t, apperr.Validation("Tag name is required.").
			WithFields(apperr.FieldError{Field: "name", Code: "REQUIRED", Message: "Required."})
	}
	t.Slug = catalogapi.TagSlug(t.Name)
	if t.Slug == "" {
		return t, apperr.Validation("Tag name needs at least one letter or number.").
			WithFields(apperr.FieldError{Field: "name", Code: "INVALID", Message: "Letters or numbers."})
	}
	switch t.Status {
	case "":
		t.Status = catalogapi.TagActive
	case catalogapi.TagActive, catalogapi.TagInactive:
	default:
		return t, apperr.Validation("status must be active or inactive.").
			WithFields(apperr.FieldError{Field: "status", Code: "INVALID", Message: "active or inactive."})
	}
	if in.Restricted {
		if in.RestrictedFrom == nil || in.RestrictedTo == nil {
			return t, apperr.Validation("Enter the restricted dates: a start and an end.").
				WithFields(apperr.FieldError{Field: "restrictedFrom", Code: "REQUIRED", Message: "Start and end required."})
		}
		if !in.RestrictedTo.After(*in.RestrictedFrom) {
			return t, apperr.Validation("The restricted end must be after the start.").
				WithFields(apperr.FieldError{Field: "restrictedTo", Code: "INVALID", Message: "Must be after the start."})
		}
		if in.RepeatYearly && in.RestrictedTo.Sub(*in.RestrictedFrom) >= 365*24*time.Hour {
			return t, apperr.Validation("A window that repeats every year must be shorter than a year.").
				WithFields(apperr.FieldError{Field: "restrictedTo", Code: "INVALID", Message: "Shorter than a year."})
		}
		t.Restricted, t.RestrictedFrom, t.RestrictedTo = true, in.RestrictedFrom, in.RestrictedTo
	} else {
		t.RepeatYearly = false
	}
	return t, nil
}

func tagWriteError(err error) error {
	switch {
	case errors.Is(err, store.ErrDuplicate):
		return apperr.New("TAG_NAME_TAKEN", apperr.CategoryConflict, "A tag with that name already exists.").
			WithFields(apperr.FieldError{Field: "name", Code: "TAKEN", Message: "Already used."})
	case errors.Is(err, store.ErrNotFound):
		return apperr.NotFound("Tag not found.")
	}
	return apperr.Internal(err)
}

func trimmedPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

// applyProductTags maps a saved product's tags: the ids sent, or — for a
// writer still sending tag text (the import, older clients) — the tags those
// names stand for, created when new. Nothing sent leaves them unchanged.
func (s *Service) applyProductTags(ctx context.Context, productID string, tagIDs, occasionNames []string, occasionSent bool) error {
	ids := tagIDs
	if ids == nil {
		if !occasionSent {
			return nil
		}
		var err error
		if ids, err = s.store.TagIDsForSlugs(ctx, occasionNames); err != nil {
			return apperr.Internal(err)
		}
	}
	if err := s.store.SetProductTags(ctx, productID, ids); err != nil {
		if errors.Is(err, store.ErrRelatedMissing) {
			return apperr.Validation("One of the seasonal tags no longer exists. Choose from the list.").
				WithFields(apperr.FieldError{Field: "seasonalTagIds", Code: "NOT_FOUND", Message: "Unknown tag."})
		}
		return apperr.Internal(err)
	}
	return nil
}

// RestrictionsForProducts implements catalogapi.TagRestrictions for orders.
func (s *Service) RestrictionsForProducts(ctx context.Context, productIDs []string) (map[string][]catalogapi.TagRestriction, error) {
	return s.store.RestrictionsForProducts(ctx, productIDs)
}
