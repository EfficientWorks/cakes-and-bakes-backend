package domain

import (
	"context"
	"strings"
	"unicode/utf8"

	"bakehouse/pkg/apperr"
	"bakehouse/services/catalog/catalogapi"
)

// normaliseProductOptions puts a product's code, colours, sponges and cake
// message settings into their stored form, or says what is wrong (F2, F7, F8).
//
// Runs on the product as it will be saved — after a PATCH is merged — so a
// partial update is checked against the whole record.
func normaliseProductOptions(p *catalogapi.Product) error {
	if p.Code != nil {
		code, ok := catalogapi.NormaliseProductCode(*p.Code)
		if !ok {
			return apperr.Validation("The product code must be 32 characters or fewer.").
				WithFields(apperr.FieldError{Field: "code", Code: "INVALID", Message: "32 characters or fewer."})
		}
		if code == "" {
			p.Code = nil
		} else {
			p.Code = &code
		}
	}

	colours, bad := catalogapi.NormaliseColours(p.Colours)
	if len(bad) > 0 {
		return apperr.Validation("Unknown colour: " + strings.Join(bad, ", ") + ". Use " + strings.Join(catalogapi.ProductColours, ", ") + ".").
			WithFields(apperr.FieldError{Field: "colours", Code: "INVALID", Message: "Unknown colour."})
	}
	p.Colours = colours
	sponges, bad := catalogapi.NormaliseSponges(p.Sponges)
	if len(bad) > 0 {
		return apperr.Validation("Unknown sponge: " + strings.Join(bad, ", ") + ". Use " + strings.Join(catalogapi.ProductSponges, ", ") + ".").
			WithFields(apperr.FieldError{Field: "sponges", Code: "INVALID", Message: "Unknown sponge."})
	}
	p.Sponges = sponges

	p.CakeMessageMode = strings.ToLower(strings.TrimSpace(p.CakeMessageMode))
	if p.CakeMessageMode == "" {
		p.CakeMessageMode = catalogapi.OptionOff
	}
	if !catalogapi.ValidOptionMode(p.CakeMessageMode) {
		return apperr.Validation("cakeMessageMode must be off, optional or required.").
			WithFields(apperr.FieldError{Field: "cakeMessageMode", Code: "INVALID", Message: "off, optional or required."})
	}
	if p.CakeMessageMaxLength == 0 {
		p.CakeMessageMaxLength = catalogapi.DefaultCakeMessageMaxLength
	}
	if p.CakeMessageMaxLength < catalogapi.MinCakeMessageMaxLength || p.CakeMessageMaxLength > catalogapi.MaxCakeMessageMaxLength {
		return apperr.Validation("cakeMessageMaxLength must be between 1 and 200.").
			WithFields(apperr.FieldError{Field: "cakeMessageMaxLength", Code: "INVALID", Message: "Between 1 and 200."})
	}
	if p.CakeMessagePresets == nil {
		p.CakeMessagePresets = catalogapi.DefaultCakeMessagePresets()
	}
	p.CakeMessagePresets = catalogapi.NormalisePresets(p.CakeMessagePresets)
	if len(p.CakeMessagePresets) > catalogapi.MaxCakeMessagePresets {
		return apperr.Validation("A product can offer at most 20 message presets.").
			WithFields(apperr.FieldError{Field: "cakeMessagePresets", Code: "INVALID", Message: "At most 20."})
	}
	for _, preset := range p.CakeMessagePresets {
		if utf8.RuneCountInString(preset) > p.CakeMessageMaxLength {
			return apperr.Validation("The preset \"" + preset + "\" is longer than the message limit.").
				WithFields(apperr.FieldError{Field: "cakeMessagePresets", Code: "TOO_LONG", Message: "Longer than the message limit."})
		}
	}
	return nil
}

// checkProductCode refuses a code another live product of the business uses.
// The unique index is the guarantee; this gives the console a clear answer
// before the write.
func (s *Service) checkProductCode(ctx context.Context, code *string, excludeID string) error {
	if code == nil {
		return nil
	}
	taken, err := s.store.ProductCodeTaken(ctx, *code, excludeID)
	if err != nil {
		return apperr.Internal(err)
	}
	if taken {
		return errCodeTaken()
	}
	return nil
}

func errCodeTaken() error {
	return apperr.New("PRODUCT_CODE_TAKEN", apperr.CategoryConflict,
		"Another product already uses that product code.").
		WithFields(apperr.FieldError{Field: "code", Code: "TAKEN", Message: "Already in use."})
}
