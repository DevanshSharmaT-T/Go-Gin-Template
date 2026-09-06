// File: internal/config/example_test.go

package config

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// README.md and docs/ARCHITECTURE.md both tell operators that .env.example is
// *the* configuration reference. These tests keep that true mechanically, so
// the promise cannot rot as keys are added.
//
// A key read but not documented is invisible to whoever deploys this. A key
// documented but not read is a setting someone will spend an afternoon
// discovering does nothing.

// exampleKeys parses the assignments out of .env.example.
func exampleKeys(t *testing.T) map[string]string {
	t.Helper()

	path := filepath.Join("..", "..", ".env.example")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()

	keys := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		// Strip the trailing "# [default: ...]" annotation.
		if comment := strings.Index(value, "#"); comment >= 0 {
			value = value[:comment]
		}
		keys[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(keys) == 0 {
		t.Fatal(".env.example parsed to zero keys")
	}
	return keys
}

// keysReadByLoader runs a successful Load and returns the variables it touched.
func keysReadByLoader(t *testing.T) map[string]bool {
	t.Helper()
	isolate(t)
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	read := map[string]bool{}
	for _, key := range cfg.keysRead {
		read[key] = true
	}
	return read
}

func TestEnvExample_DocumentsEveryKeyTheLoaderReads(t *testing.T) {
	documented := exampleKeys(t)
	read := keysReadByLoader(t)

	var missing []string
	for key := range read {
		if _, present := documented[key]; !present {
			missing = append(missing, key)
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("read by the loader but absent from .env.example:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

func TestEnvExample_HasNoKeyTheLoaderIgnores(t *testing.T) {
	documented := exampleKeys(t)
	read := keysReadByLoader(t)

	// TEST_DATABASE_URL is consumed by the test suites, not by Load.
	ignoredByDesign := map[string]bool{"TEST_DATABASE_URL": true}

	var orphaned []string
	for key := range documented {
		if !read[key] && !ignoredByDesign[key] {
			orphaned = append(orphaned, key)
		}
	}

	sort.Strings(orphaned)
	if len(orphaned) > 0 {
		t.Errorf("documented in .env.example but never read — either wire them up or remove them:\n  %s",
			strings.Join(orphaned, "\n  "))
	}
}

func TestEnvExample_ContainsNoRealCredential(t *testing.T) {
	// This file is committed. It must stay placeholder-only.
	documented := exampleKeys(t)

	for _, key := range []string{"JWT_SECRET", "ADMIN_PASSWORD"} {
		value := documented[key]
		if value == "" {
			t.Errorf("%s should carry an obvious placeholder, not be blank", key)
			continue
		}
		if !strings.Contains(strings.ToUpper(value), placeholderMarker) {
			t.Errorf("%s = %q does not look like a placeholder; the loader rejects "+
				"values containing %q, and that check is what stops a copied "+
				"example file from booting", key, value, placeholderMarker)
		}
	}

	for _, key := range []string{"SMTP_PASSWORD", "SMTP_USERNAME", "GOOGLE_CLIENT_SECRET", "SENTRY_DSN"} {
		if value := documented[key]; value != "" {
			t.Errorf("%s must ship empty, got %q", key, value)
		}
	}
}

func TestEnvExample_IsUsableAsWritten(t *testing.T) {
	// Copying .env.example to .env and editing only the two required secrets is
	// the documented quick start. Everything else it ships must validate.
	documented := exampleKeys(t)
	isolate(t)

	for key, value := range documented {
		if key == "TEST_DATABASE_URL" {
			continue
		}
		if key == "JWT_SECRET" {
			value = strings.Repeat("k", MinJWTSecretLength)
		}
		t.Setenv(key, value)
	}

	if _, err := Load(); err != nil {
		t.Fatalf(".env.example does not validate as shipped:\n%v", err)
	}
}
