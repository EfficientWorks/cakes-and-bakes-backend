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
