package domain

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store"
)

/*
 * Flavours: the home page's "Discover our flavours" cards. Edited by whoever
 * may edit the master catalogue, like seasonal tags. See catalogapi/flavours.go.
 */

// Limits keep a card readable in the design's layout.
const (
	flavourNameMax        = 60
	flavourDescriptionMax = 200
)

var hexColour = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// FlavourInput is a flavour as admin saves it. ProductIDs nil leaves the
// mapped products alone on an update; an empty list unmaps them all.
type FlavourInput struct {
	Name             string
	Description      *string
	ImageURL         *string
	ImageAltText     *string
	BackgroundColour *string
	SortOrder        int
	Status           string
	ProductIDs       []string
}

func (s *Service) ListFlavours(ctx context.Context, status, search string) ([]catalogapi.Flavour, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "all" {
		status = ""
	}
	if status != "" && status != catalogapi.FlavourActive && status != catalogapi.FlavourInactive {
		return nil, apperr.Validation("status must be active or inactive.")
	}
	out, err := s.store.ListFlavours(ctx, status, search)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

// StorefrontFlavours are the home page's cards: active, with products to show.
func (s *Service) StorefrontFlavours(ctx context.Context) ([]catalogapi.Flavour, error) {
	out, err := s.store.ListFlavoursInUse(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) GetFlavour(ctx context.Context, id string) (*catalogapi.Flavour, error) {
	f, err := s.store.GetFlavour(ctx, id)
	if err != nil {
		return nil, flavourWriteError(err)
	}
	return f, nil
}

func (s *Service) CreateFlavour(ctx context.Context, claims *auth.StaffClaims, in FlavourInput) (*catalogapi.Flavour, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return nil, apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	f, err := flavourFromInput(in)
	if err != nil {
		return nil, err
	}
	if f.ProductIDs == nil {
		f.ProductIDs = []string{}
	}
	out, err := s.store.CreateFlavour(ctx, f)
	if err != nil {
		return nil, flavourWriteError(err)
	}
	s.writeAudit(ctx, claims, "flavour.create", "flavour", &out.ID, nil, nil, out)
	return out, nil
}

func (s *Service) UpdateFlavour(ctx context.Context, claims *auth.StaffClaims, id string, in FlavourInput) (*catalogapi.Flavour, error) {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return nil, apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	before, err := s.store.GetFlavour(ctx, id)
	if err != nil {
		return nil, flavourWriteError(err)
	}
	f, err := flavourFromInput(in)
	if err != nil {
		return nil, err
	}
	f.ID = before.ID
	out, err := s.store.UpdateFlavour(ctx, f)
	if err != nil {
		return nil, flavourWriteError(err)
	}
	s.writeAudit(ctx, claims, "flavour.update", "flavour", &out.ID, nil, before, out)
	return out, nil
}

func (s *Service) DeleteFlavour(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermCatalogueEditMaster) {
		return apperr.PermissionDenied("Only super admin can edit the master catalogue.")
	}
	if err := s.store.DeleteFlavour(ctx, id); err != nil {
		return flavourWriteError(err)
	}
	s.writeAudit(ctx, claims, "flavour.delete", "flavour", &id, nil, nil, nil)
	return nil
}

// flavourFromInput validates and tidies what admin sent.
func flavourFromInput(in FlavourInput) (catalogapi.Flavour, error) {
	f := catalogapi.Flavour{
		Name: strings.TrimSpace(in.Name), Description: trimmedPtr(in.Description),
		ImageURL: trimmedPtr(in.ImageURL), ImageAltText: trimmedPtr(in.ImageAltText),
		BackgroundColour: trimmedPtr(in.BackgroundColour), SortOrder: in.SortOrder,
		Status: strings.ToLower(strings.TrimSpace(in.Status)), ProductIDs: in.ProductIDs,
	}
	field := func(name, code, msg, long string) error {
		return apperr.Validation(long).WithFields(apperr.FieldError{Field: name, Code: code, Message: msg})
	}
	if f.Name == "" {
		return f, field("name", "REQUIRED", "Required.", "Flavour name is required.")
	}
	if utf8.RuneCountInString(f.Name) > flavourNameMax {
		return f, field("name", "TOO_LONG", "60 characters at most.", "Flavour name must be 60 characters or fewer.")
	}
	f.Slug = catalogapi.TagSlug(f.Name)
	if f.Slug == "" {
		return f, field("name", "INVALID", "Letters or numbers.", "Flavour name needs at least one letter or number.")
	}
	if f.Description != nil && utf8.RuneCountInString(*f.Description) > flavourDescriptionMax {
		return f, field("description", "TOO_LONG", "200 characters at most.", "Description must be 200 characters or fewer.")
	}
	if f.BackgroundColour != nil {
		if !hexColour.MatchString(*f.BackgroundColour) {
			return f, field("backgroundColour", "INVALID", "#RRGGBB.", "Card colour must be a hex colour like #F8E8E8.")
		}
		c := strings.ToUpper(*f.BackgroundColour)
		f.BackgroundColour = &c
	}
	if f.SortOrder < 0 {
		return f, field("sortOrder", "INVALID", "0 or more.", "Position must be 0 or more.")
	}
	switch f.Status {
	case "":
		f.Status = catalogapi.FlavourActive
	case catalogapi.FlavourActive, catalogapi.FlavourInactive:
	default:
		return f, field("status", "INVALID", "active or inactive.", "status must be active or inactive.")
	}
	return f, nil
}

func flavourWriteError(err error) error {
	switch {
	case errors.Is(err, store.ErrDuplicate):
		return apperr.New("FLAVOUR_NAME_TAKEN", apperr.CategoryConflict, "A flavour with that name already exists.").
			WithFields(apperr.FieldError{Field: "name", Code: "TAKEN", Message: "Already used."})
	case errors.Is(err, store.ErrRelatedMissing):
		return apperr.Validation("One of the products could not be found.").
			WithFields(apperr.FieldError{Field: "productIds", Code: "NOT_FOUND", Message: "Unknown product."})
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrMalformedID):
		return apperr.NotFound("Flavour not found.")
	}
	return apperr.Internal(err)
}
