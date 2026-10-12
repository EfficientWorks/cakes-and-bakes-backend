// Package flags is a small environment-backed feature-flag lookup.
//
// It exists so a half-built capability can ship dark behind a named switch
// instead of behind a branch that never merges. Flags are read once at
// startup and are immutable for the life of the process — a flag that has
// to change without a restart is a configuration record in Postgres, not
// this.
package flags

import (
	"os"
	"sort"
	"strings"
)

// Prefix is the environment-variable prefix a flag must carry.
// FEATURE_KDS_LIVE=true enables the flag named "kds_live".
const Prefix = "FEATURE_"

// Flags is a resolved set of feature switches.
type Flags map[string]bool

// FromEnv reads every FEATURE_* variable in the environment. Values are
// case-insensitive; "1", "true", "yes" and "on" all enable a flag, anything
// else disables it.
func FromEnv() Flags {
	f := Flags{}
	for _, kv := range os.Environ() {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(key, Prefix) {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(key, Prefix))
		if name == "" {
			continue
		}
		f[name] = truthy(value)
	}
	return f
}

// On reports whether a flag is enabled. An unset flag is off, so the safe
// default is always "the old behaviour".
func (f Flags) On(name string) bool { return f[strings.ToLower(name)] }

// Enabled lists the enabled flags, sorted. Log this at startup so a
// surprising environment is visible in the first few lines of output.
func (f Flags) Enabled() []string {
	out := make([]string, 0, len(f))
	for name, on := range f {
		if on {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
