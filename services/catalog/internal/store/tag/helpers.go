package tag

import "strings"

// displayTagName turns "thank_you" or "new-baby" into "Thank You", "New Baby".
func displayTagName(n string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(strings.TrimSpace(n)))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}
	return strings.Join(words, " ")
}
