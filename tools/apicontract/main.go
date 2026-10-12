// Command apicontract guards the API contract of clients we do not ship with
// the backend — the POS and KDS apps. It reads the generated Swagger spec,
// flattens every request and response shape under the guarded path prefixes
// (resolving $refs inline, so renaming a Go package does not count as a
// change), and compares the result with a committed snapshot.
//
//	go run ./tools/apicontract            # fail if the contract changed
//	go run ./tools/apicontract -update    # accept the current contract
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

var guardedPrefixes = []string{"/pos", "/kds"}

const (
	specPath     = "api/swagger.json"
	snapshotPath = "api/contracts/pos-kds.json"
)

type spec struct {
	Paths       map[string]map[string]operation `json:"paths"`
	Definitions map[string]any                  `json:"definitions"`
}

type operation struct {
	Parameters []map[string]any `json:"parameters"`
	Responses  map[string]any   `json:"responses"`
}

func main() {
	update := flag.Bool("update", false, "write the current contract as the new snapshot")
	flag.Parse()

	current, err := buildContract()
	if err != nil {
		fail(err)
	}

	if *update {
		if err := os.WriteFile(snapshotPath, current, 0o644); err != nil {
			fail(err)
		}
		fmt.Println("contract snapshot updated:", snapshotPath)
		return
	}

	saved, err := os.ReadFile(snapshotPath)
	if err != nil {
		fail(fmt.Errorf("read snapshot (run with -update to create it): %w", err))
	}
	if diff := diffContracts(saved, current); len(diff) > 0 {
		fmt.Println("POS/KDS API contract changed — these clients are not redeployed with the backend:")
		for _, line := range diff {
			fmt.Println("  " + line)
		}
		fmt.Println("If the change is intended and the apps are updated, run: go run ./tools/apicontract -update")
		os.Exit(1)
	}
	fmt.Println("POS/KDS API contract unchanged")
}

// buildContract returns the guarded endpoints as stable, indented JSON keyed
// by "METHOD /path".
func buildContract() ([]byte, error) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, err
	}
	var s spec
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", specPath, err)
	}

	contract := map[string]any{}
	for path, methods := range s.Paths {
		if !isGuarded(path) {
			continue
		}
		for method, op := range methods {
			contract[strings.ToUpper(method)+" "+path] = map[string]any{
				"parameters": flattenParameters(op.Parameters, s.Definitions),
				"responses":  resolve(op.Responses, s.Definitions, nil),
			}
		}
	}
	return json.MarshalIndent(contract, "", "  ")
}

func isGuarded(path string) bool {
	for _, p := range guardedPrefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// flattenParameters drops descriptions (documentation, not contract) and
// sorts parameters so their order in the annotations does not matter.
func flattenParameters(params []map[string]any, defs map[string]any) []any {
	out := make([]any, 0, len(params))
	for _, p := range params {
		out = append(out, resolve(p, defs, nil))
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]) < fmt.Sprint(out[j])
	})
	return out
}

// resolve inlines $refs and strips keys that describe rather than define the
// shape. seen guards against self-referencing definitions.
func resolve(node any, defs map[string]any, seen []string) any {
	switch v := node.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/definitions/")
			for _, s := range seen {
				if s == name {
					return map[string]any{"$cycle": shortName(name)}
				}
			}
			return resolve(defs[name], defs, append(seen, name))
		}
		out := map[string]any{}
		for k, val := range v {
			switch k {
			case "description", "example", "x-example", "title":
				continue
			}
			out[k] = resolve(val, defs, seen)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = resolve(val, defs, seen)
		}
		return out
	default:
		return v
	}
}

// shortName drops the Go package prefix swag puts on definition names.
func shortName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// diffContracts lists endpoints that were added, removed or changed.
func diffContracts(savedRaw, currentRaw []byte) []string {
	var saved, current map[string]any
	_ = json.Unmarshal(savedRaw, &saved)
	_ = json.Unmarshal(currentRaw, &current)

	var lines []string
	for key, s := range saved {
		c, ok := current[key]
		switch {
		case !ok:
			lines = append(lines, "removed: "+key)
		case !equalJSON(s, c):
			lines = append(lines, "changed: "+key)
		}
	}
	for key := range current {
		if _, ok := saved[key]; !ok {
			lines = append(lines, "added:   "+key)
		}
	}
	sort.Strings(lines)
	return lines
}

func equalJSON(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "apicontract:", err)
	os.Exit(2)
}
