package config

import "testing"

func TestParseDotEnvLine(t *testing.T) {
	cases := []struct {
		name, line, key, value string
		ok                     bool
	}{
		{"plain", "STRIPE_SECRET_KEY=sk_test_abc", "STRIPE_SECRET_KEY", "sk_test_abc", true},
		{"spaces around equals", "  PORT = 8080 ", "PORT", "8080", true},
		{"export prefix", "export APP_ENV=local", "APP_ENV", "local", true},
		{"double quoted", `FROM="Baked Bouquet <a@b.com>"`, "FROM", "Baked Bouquet <a@b.com>", true},
		{"single quoted keeps hash", `PASS='p#ss word'`, "PASS", "p#ss word", true},
		{"inline comment stripped", "PORT=8080 # the http port", "PORT", "8080", true},
		{"hash inside value kept", "PASS=pa#ssword", "PASS", "pa#ssword", true},
		{"url with query", "DB=postgres://u:p@h/db?sslmode=disable", "DB", "postgres://u:p@h/db?sslmode=disable", true},
		{"empty value", "EMPTY=", "EMPTY", "", true},
		{"comment line", "# STRIPE_SECRET_KEY=x", "", "", false},
		{"blank line", "   ", "", "", false},
		{"no equals", "JUST_A_WORD", "", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, value, ok := parseDotEnvLine(c.line)
			if ok != c.ok || key != c.key || value != c.value {
				t.Errorf("parseDotEnvLine(%q) = (%q, %q, %v), want (%q, %q, %v)",
					c.line, key, value, ok, c.key, c.value, c.ok)
			}
		})
	}
}

// A variable already present in the environment must survive: in production
// the platform injects secrets and a stray .env must not override them.
func TestLoadDotEnvDoesNotOverrideRealEnv(t *testing.T) {
	t.Setenv("BB_DOTENV_TEST_KEY", "from-environment")

	dir := t.TempDir()
	path := dir + "/.env"
	if err := writeFile(path, "BB_DOTENV_TEST_KEY=from-file\nBB_DOTENV_TEST_NEW=from-file\n"); err != nil {
		t.Fatal(err)
	}

	loadDotEnv(path)

	if got := getEnv("BB_DOTENV_TEST_KEY", ""); got != "from-environment" {
		t.Errorf("environment value was overwritten by the file: got %q", got)
	}
	if got := getEnv("BB_DOTENV_TEST_NEW", ""); got != "from-file" {
		t.Errorf("unset key was not filled from the file: got %q", got)
	}
}
