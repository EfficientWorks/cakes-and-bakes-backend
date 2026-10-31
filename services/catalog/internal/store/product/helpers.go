package product

import "bakehouse/services/catalog/catalogapi"

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// prepUnit stores a preparation-time unit in its canonical spelling.
func prepUnit(u string) string {
	if v, ok := catalogapi.NormalizePrepUnit(u); ok {
		return v
	}
	return "minutes"
}

// cakeMode stores an empty message mode as off.
func cakeMode(m string) string {
	if m == "" {
		return catalogapi.OptionOff
	}
	return m
}

// cakeMaxLen stores a zero message limit as the default.
func cakeMaxLen(n int) int {
	if n <= 0 {
		return catalogapi.DefaultCakeMessageMaxLength
	}
	return n
}

// presetsOrDefault gives a product written without presets the default chips;
// an explicitly empty list stays empty.
func presetsOrDefault(p []string) []string {
	if p == nil {
		return catalogapi.DefaultCakeMessagePresets()
	}
	return p
}

// categoryIDs is the filter's category multi-select with the single id
// folded in, blanks and duplicates dropped.
func categoryIDs(f catalogapi.ProductFilter) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range append([]string{f.CategoryID}, f.CategoryIDs...) {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
