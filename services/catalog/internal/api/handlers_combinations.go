package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

// combinationOptionDTO is one value of an option set, with the price it adds.
type combinationOptionDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"priceCents"`
}

// combinationOptionSetDTO is one variation group the product carries.
type combinationOptionSetDTO struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	DisplayName string                 `json:"displayName"`
	Options     []combinationOptionDTO `json:"options"`
}

// adminCombinationDTO is one row of the console's Variations Set table.
type adminCombinationDTO struct {
	ID        string   `json:"id"`
	OptionIDs []string `json:"optionIds"`
	// Label is "8 Inch | Vanilla Eggless | Vanilla Fresh Cream"; empty when
	// the row no longer matches the product's option sets (isValid false).
	Label string `json:"label"`
	// AutoPriceCents is the sum of the options' prices; PriceCents is the
	// price staff typed, null when the row uses the automatic price.
	AutoPriceCents      int64  `json:"autoPriceCents"`
	PriceCents          *int64 `json:"priceCents"`
	EffectivePriceCents int64  `json:"effectivePriceCents"`
	IsActive            bool   `json:"isActive"`
	IsValid             bool   `json:"isValid"`
}

type adminCombinationsResponse struct {
	OptionSets   []combinationOptionSetDTO `json:"optionSets"`
	Combinations []adminCombinationDTO     `json:"combinations"`
}

func combinationOptionSets(p *catalogapi.Product) []combinationOptionSetDTO {
	out := []combinationOptionSetDTO{}
	for _, g := range p.ModifierGroups {
		if g.Kind != catalogapi.ModifierKindVariation || len(g.Options) == 0 {
			continue
		}
		set := combinationOptionSetDTO{ID: g.ID, Name: g.Name, DisplayName: g.DisplayName, Options: []combinationOptionDTO{}}
		if strings.TrimSpace(set.DisplayName) == "" {
			set.DisplayName = g.Name
		}
		for _, o := range g.Options {
			set.Options = append(set.Options, combinationOptionDTO{ID: o.ID, Name: o.Name, PriceCents: o.PriceDeltaCents})
		}
		out = append(out, set)
	}
	return out
}

func adminCombinationsDTO(p *catalogapi.Product) adminCombinationsResponse {
	out := adminCombinationsResponse{OptionSets: combinationOptionSets(p), Combinations: []adminCombinationDTO{}}
	for _, c := range p.Combinations {
		out.Combinations = append(out.Combinations, adminCombinationDTO{
			ID: c.ID, OptionIDs: orEmpty(c.OptionIDs), Label: p.CombinationLabel(c),
			AutoPriceCents: p.CombinationAutoPrice(c), PriceCents: c.PriceOverrideCents,
			EffectivePriceCents: p.CombinationPrice(c), IsActive: c.IsActive, IsValid: p.CombinationValid(c),
		})
	}
	return out
}

// @Summary		A product's option-set combinations
// @Description	The Variations Set screen: the variation groups (option sets) the product carries with each option's price, and every combination row with its automatic price (sum of its options), the typed price if any, and On/Off. Rows that no longer match the product's option sets come back with isValid false.
// @Tags			admin-catalogue
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Product id"
// @Success		200	{object}	httpx.Envelope{data=adminCombinationsResponse}
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/products/{id}/combinations [get]
func (h *Handler) GetProductCombinations(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.ProductCombinations(r.Context(), staffClaims(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, adminCombinationsDTO(p))
}

type combinationRowRequest struct {
	// One option id from each option set on the product.
	OptionIDs []string `json:"optionIds"`
	// Omit or send null for the automatic price (sum of the options' prices).
	PriceCents *int64 `json:"priceCents" example:"3499"`
	IsActive   bool   `json:"isActive" example:"true"`
}

type setCombinationsRequest struct {
	Combinations []combinationRowRequest `json:"combinations"`
}

// @Summary		Replace a product's option-set combinations
// @Description	Saves the Variations Set table as a whole: rows sent are kept (matched by their options, so ids survive), rows left out are deleted. Attach the option sets first with PUT /admin/products/{id}/modifiers — every row needs exactly one option from each variation group the product carries. An empty list removes every row and the product is priced the old way again. No stock is tracked; isActive false hides a row everywhere.
// @Tags			admin-catalogue
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Product id"
// @Param			body	body		setCombinationsRequest	true	"Rows"
// @Success		200		{object}	httpx.Envelope{data=adminCombinationsResponse}
// @Failure		400		{object}	httpx.Envelope	"INVALID_COMBINATION | DUPLICATE_COMBINATION | INVALID_PRICE | TOO_MANY_COMBINATIONS"
// @Failure		404		{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/products/{id}/combinations [put]
func (h *Handler) SetProductCombinations(w http.ResponseWriter, r *http.Request) {
	var req setCombinationsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rows := make([]domain.CombinationInput, 0, len(req.Combinations))
	for _, c := range req.Combinations {
		rows = append(rows, domain.CombinationInput{OptionIDs: c.OptionIDs, PriceCents: c.PriceCents, IsActive: c.IsActive})
	}
	p, err := h.svc.SetProductCombinations(r.Context(), staffClaims(r), chi.URLParam(r, "id"), rows)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, adminCombinationsDTO(p))
}

// storefrontCombinationDTO is one row a customer may pick: only rows that are
// On and still valid are sent, so the product page can grey out everything
// else.
type storefrontCombinationDTO struct {
	ID         string   `json:"id"`
	OptionIDs  []string `json:"optionIds"`
	Label      string   `json:"label"`
	PriceCents int64    `json:"priceCents"`
}

func storefrontCombinations(p *catalogapi.Product) []storefrontCombinationDTO {
	out := []storefrontCombinationDTO{}
	for _, c := range p.SellableCombinations() {
		out = append(out, storefrontCombinationDTO{
			ID: c.ID, OptionIDs: orEmpty(c.OptionIDs), Label: p.CombinationLabel(c), PriceCents: p.CombinationPrice(c),
		})
	}
	return out
}
