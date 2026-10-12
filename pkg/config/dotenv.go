package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv reads a `.env` file into the process environment.
//
// Without this, `.env` is a file the application never reads — which is worse
// than not having one, because the keys look configured. Mail and payment
// credentials sat in it while the server logged that the keys were not set
// and quietly swapped in a no-op payment processor and a mailer that only
// writes to the log. Registration emails were never sent and no card was ever
// charged, with nothing failing loudly enough to notice.
//
// Deliberately not a dependency. The format this needs to read is a handful of
// KEY=value lines; pulling in a library for that is more supply chain than the
// problem deserves.
//
// A real environment variable always wins over a file entry. That is the whole
// point in production, where the platform injects secrets and a stray `.env`
// left in an image must not override them.
func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		// Absent is the normal case in production. Not an error.
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		// LookupEnv rather than Getenv: a variable deliberately set to empty
		// is still set, and the file must not quietly refill it.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

// parseDotEnvLine pulls a KEY=value pair out of one line, or reports that
// there is nothing there to take.
func parseDotEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	// `export FOO=bar` is common in files meant to be sourced by a shell.
	line = strings.TrimPrefix(line, "export ")

	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}

	value = strings.TrimSpace(value)

	// Quoted values keep everything inside the quotes, including the `#` that
	// would otherwise start a comment and any trailing spaces that matter.
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			return key, value[1 : len(value)-1], true
		}
	}

	// Unquoted: an inline comment ends the value. Requiring the whitespace
	// before `#` keeps a literal fragment like `pass#word` intact.
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}

	return key, value, true
}

// loadDotEnvFromRoot finds `.env` by walking up from the working directory.
//
// The binary is run from the repo root by `make run`, but tests and tools run
// from their own package directories, where a bare `.env` would not be found.
// Walking up means every entry point sees the same configuration instead of
// silently getting defaults.
func loadDotEnvFromRoot() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}

	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			loadDotEnv(candidate)
			return
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return // reached the filesystem root
		}
		dir = parent
	}
}
