package catalogapi

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

/*
 * Per-product options a cake shop needs: a product code, the colours and
 * sponges it can be filtered by, and a message piped on the cake.
 *
 * The rules live here rather than in the catalogue's domain because orders
 * applies the same ones to a cart line (a message too long for the product)
 * and imports only catalogapi.
 */

// Cake message modes.
const (
	OptionOff      = "off"
	OptionOptional = "optional"
	OptionRequired = "required"
)

// ValidOptionMode reports whether v is off, optional or required.
func ValidOptionMode(v string) bool {
	return v == OptionOff || v == OptionOptional || v == OptionRequired
}

// Cake message length bounds, in characters (runes), not bytes.
const (
	DefaultCakeMessageMaxLength = 40
	MinCakeMessageMaxLength     = 1
	MaxCakeMessageMaxLength     = 200
	// MaxCakeMessagePresets caps the chips a product page shows.
	MaxCakeMessagePresets = 20
	// MaxProductCodeLength is the longest product code, e.g. "WPC-040".
	MaxProductCodeLength = 32
)

// DefaultCakeMessagePresets are the chips a new product starts with.
func DefaultCakeMessagePresets() []string {
	return []string{"Happy Birthday", "Happy Anniversary", "Congratulations", "Happy Wedding Anniversary"}
}

// ProductColours and ProductSponges are the only values a product may carry,
// in the order the filter panel lists them.
var (
	ProductColours = []string{"Pink", "Blue", "Yellow", "Gold", "White", "Red", "Green", "Black", "Purple", "Silver", "Multicolour"}
	ProductSponges = []string{"Chocolate", "Vanilla", "Red Velvet", "Fruit", "Lemon", "Coffee", "Butterscotch", "Black Forest", "Pineapple", "Strawberry"}
)

// NormaliseProductCode trims and upper-cases a code. Empty means "no code".
// ok is false when the code is too long or carries control characters.
func NormaliseProductCode(raw string) (code string, ok bool) {
	code = strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return "", true
	}
	if utf8.RuneCountInString(code) > MaxProductCodeLength {
		return code, false
	}
	for _, r := range code {
		if unicode.IsControl(r) {
			return code, false
		}
	}
	return code, true
}

// NormaliseColours maps each value onto its canonical spelling
// (case-insensitive, "multicolor" and "multi-colour" accepted), dropping
// blanks and duplicates. invalid lists what matched nothing.
func NormaliseColours(in []string) (out []string, invalid []string) {
	return normaliseAllowed(in, ProductColours, map[string]string{
		"multicolor": "Multicolour", "multi-colour": "Multicolour", "multi colour": "Multicolour",
		"multi-color": "Multicolour", "grey": "Silver", "gray": "Silver",
	})
}

// NormaliseSponges is NormaliseColours for sponge flavours.
func NormaliseSponges(in []string) (out []string, invalid []string) {
	return normaliseAllowed(in, ProductSponges, map[string]string{
		"redvelvet": "Red Velvet", "red-velvet": "Red Velvet", "blackforest": "Black Forest", "black-forest": "Black Forest",
	})
}

func normaliseAllowed(in, allowed []string, aliases map[string]string) (out []string, invalid []string) {
	byKey := make(map[string]string, len(allowed)+len(aliases))
	for _, a := range allowed {
		byKey[strings.ToLower(a)] = a
	}
	for k, v := range aliases {
		byKey[k] = v
	}
	seen := map[string]bool{}
	out = []string{}
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		canon, ok := byKey[strings.ToLower(strings.Join(strings.Fields(v), " "))]
		if !ok {
			invalid = append(invalid, v)
			continue
		}
		if !seen[canon] {
			seen[canon] = true
			out = append(out, canon)
		}
	}
	return out, invalid
}

// NormalisePresets trims the preset chips and drops blanks and duplicates.
func NormalisePresets(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}

// Cake message failures. Orders maps them to its error codes.
var (
	ErrCakeMessageRequired   = errors.New("cake message required")
	ErrCakeMessageTooLong    = errors.New("cake message too long")
	ErrCakeMessageNotAllowed = errors.New("cake message not allowed")
)

// CakeOptions is what a cart line needs to know about its product's message
// settings.
type CakeOptions struct {
	CakeMessageMode      string
	CakeMessageMaxLength int
}

// EffectiveMessageMode is CakeMessageMode with empty read as off.
func (o CakeOptions) EffectiveMessageMode() string {
	if o.CakeMessageMode == "" {
		return OptionOff
	}
	return o.CakeMessageMode
}

// EffectiveMaxLength is CakeMessageMaxLength with zero read as the default.
func (o CakeOptions) EffectiveMaxLength() int {
	if o.CakeMessageMaxLength <= 0 {
		return DefaultCakeMessageMaxLength
	}
	return o.CakeMessageMaxLength
}

// CheckCakeMessage validates a (trimmed) message against the product's mode
// and limit. requireFilled is false for staff-entered orders, which may leave
// a required message for the kitchen to confirm by phone.
func (o CakeOptions) CheckCakeMessage(message string, requireFilled bool) error {
	message = strings.TrimSpace(message)
	switch o.EffectiveMessageMode() {
	case OptionOff:
		if message != "" {
			return ErrCakeMessageNotAllowed
		}
		return nil
	case OptionRequired:
		if message == "" && requireFilled {
			return ErrCakeMessageRequired
		}
	}
	if utf8.RuneCountInString(message) > o.EffectiveMaxLength() {
		return fmt.Errorf("%w: %d characters, limit %d", ErrCakeMessageTooLong, utf8.RuneCountInString(message), o.EffectiveMaxLength())
	}
	return nil
}

// CakeOptions is the product's message settings as a cart line sees them.
func (p Product) CakeOptions() CakeOptions {
	return CakeOptions{CakeMessageMode: p.CakeMessageMode, CakeMessageMaxLength: p.CakeMessageMaxLength}
}
