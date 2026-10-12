// Package domain holds the rules for registering and changing a business.
package domain

import (
	"context"
	"errors"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/country"
	"bakehouse/services/business/businessapi"
	"bakehouse/services/business/internal/store"
	"bakehouse/services/identity/identityapi"
)

// Service registers businesses and gives each its first login.
type Service struct {
	store  *store.Store
	owners identityapi.BusinessOwners
}

func NewService(st *store.Store, owners identityapi.BusinessOwners) *Service {
	return &Service{store: st, owners: owners}
}

// Countries lists the countries a business can be registered in.
func (s *Service) Countries() []country.Profile { return country.All() }

// RegisterBusiness creates a business. Its country fills every setting the
// caller left empty: a GB business is GBP, Europe/London, en-GB, VAT 20%
// included in prices.
func (s *Service) RegisterBusiness(ctx context.Context, in businessapi.NewBusiness) (*businessapi.Business, error) {
	profile, ok := country.Lookup(in.CountryCode)
	if !ok {
		return nil, unsupportedCountry(in.CountryCode)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fieldError("name", "REQUIRED", "Enter the business name.")
	}
	slug := Slugify(in.Slug)
	if slug == "" {
		slug = Slugify(name)
	}
	if slug == "" {
		return nil, fieldError("slug", "INVALID", "Use letters, numbers and hyphens.")
	}

	b := businessapi.Business{
		Name:              name,
		Slug:              slug,
		CountryCode:       profile.Code,
		Currency:          orDefault(strings.ToUpper(in.Currency), profile.Currency),
		Timezone:          orDefault(in.Timezone, profile.Timezone),
		Locale:            orDefault(in.Locale, profile.Locale),
		TaxMode:           orDefault(strings.ToLower(in.TaxMode), profile.TaxMode),
		TaxName:           orDefault(in.TaxName, profile.TaxName),
		DefaultTaxRatePct: profile.TaxRatePct,
		ContactEmail:      nilIfBlank(in.ContactEmail),
		ContactPhone:      nilIfBlank(in.ContactPhone),
		Status:            businessapi.StatusActive,
	}
	if in.DefaultTaxRatePct != nil {
		b.DefaultTaxRatePct = *in.DefaultTaxRatePct
	}
	if err := validateSettings(b.Currency, b.Timezone, b.TaxMode, b.DefaultTaxRatePct); err != nil {
		return nil, err
	}
	logo, website, err := validateBranding(in.LogoURL, in.WebsiteURL)
	if err != nil {
		return nil, err
	}
	b.LogoURL, b.WebsiteURL = nilIfBlank(logo), nilIfBlank(website)

	out, err := s.store.CreateBusiness(ctx, b)
	if errors.Is(err, store.ErrWebsiteTaken) {
		return nil, websiteTaken()
	}
	if errors.Is(err, store.ErrDuplicate) {
		return nil, apperr.New("BUSINESS_SLUG_TAKEN", apperr.CategoryConflict,
			"Another business already uses \""+slug+"\". Choose a different slug.").
			WithFields(apperr.FieldError{Field: "slug", Code: "TAKEN", Message: "Already in use."})
	}
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

// GetBusinessByWebsite finds the business whose shop is served from origin.
func (s *Service) GetBusinessByWebsite(ctx context.Context, origin string) (*businessapi.Business, error) {
	b, err := s.store.GetBusinessByWebsite(ctx, origin)
	if err != nil {
		return nil, notFoundOr(err)
	}
	return b, nil
}

func (s *Service) GetBusiness(ctx context.Context, id string) (*businessapi.Business, error) {
	b, err := s.store.GetBusiness(ctx, id)
	if err != nil {
		return nil, notFoundOr(err)
	}
	return b, nil
}

func (s *Service) ListBusinesses(ctx context.Context, search, status string) ([]businessapi.Business, error) {
	if status != "" && status != businessapi.StatusActive && status != businessapi.StatusSuspended {
		return nil, fieldError("status", "INVALID", "Use active or suspended.")
	}
	list, err := s.store.ListBusinesses(ctx, search, status)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return list, nil
}

// UpdateBusiness changes a business's settings. The country is fixed.
func (s *Service) UpdateBusiness(ctx context.Context, id string, p businessapi.BusinessPatch) (*businessapi.Business, error) {
	current, err := s.store.GetBusiness(ctx, id)
	if err != nil {
		return nil, notFoundOr(err)
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		return nil, fieldError("name", "REQUIRED", "Enter the business name.")
	}
	if p.Currency != nil {
		upper := strings.ToUpper(*p.Currency)
		p.Currency = &upper
	}
	if p.TaxMode != nil {
		lower := strings.ToLower(*p.TaxMode)
		p.TaxMode = &lower
	}
	if p.Status != nil && *p.Status != businessapi.StatusActive && *p.Status != businessapi.StatusSuspended {
		return nil, fieldError("status", "INVALID", "Use active or suspended.")
	}
	if err := validateSettings(
		pick(p.Currency, current.Currency), pick(p.Timezone, current.Timezone),
		pick(p.TaxMode, current.TaxMode), pickFloat(p.DefaultTaxRatePct, current.DefaultTaxRatePct),
	); err != nil {
		return nil, err
	}
	if p.LogoURL != nil || p.WebsiteURL != nil {
		logo, website, err := validateBranding(pick(p.LogoURL, ""), pick(p.WebsiteURL, ""))
		if err != nil {
			return nil, err
		}
		if p.LogoURL != nil {
			p.LogoURL = &logo
		}
		if p.WebsiteURL != nil {
			p.WebsiteURL = &website
		}
	}
	out, err := s.store.UpdateBusiness(ctx, id, p)
	if errors.Is(err, store.ErrWebsiteTaken) {
		return nil, websiteTaken()
	}
	if err != nil {
		return nil, notFoundOr(err)
	}
	return out, nil
}

// CreateOwner gives an active business a login: a super admin over that
// business, who then sets up stores, staff and the menu in the admin console.
func (s *Service) CreateOwner(ctx context.Context, businessID, email, password, firstName, lastName string) (*identityapi.StaffUser, error) {
	b, err := s.store.GetBusiness(ctx, businessID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	if b.Status != businessapi.StatusActive {
		return nil, apperr.New("BUSINESS_SUSPENDED", apperr.CategoryBusinessRule,
			"This business is suspended. Reactivate it before adding a login.")
	}
	return s.owners.CreateBusinessOwner(ctx, b.ID, email, password, firstName, lastName)
}

func notFoundOr(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return apperr.NotFound("Business not found.")
	}
	return apperr.Internal(err)
}
