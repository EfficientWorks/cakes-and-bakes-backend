package recipient

import "strings"

func normaliseEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }
