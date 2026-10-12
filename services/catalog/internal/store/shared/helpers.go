package shared

import "strings"

func LikeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func UniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// OrEmptyStrings keeps a nil slice out of a NOT NULL array column — pgx sends
// nil as SQL NULL, which the column rejects.
func OrEmptyStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// OrNilUUID turns an empty staff id into SQL NULL rather than letting an empty
// string reach a uuid column, which errors.
func OrNilUUID(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
