package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	const name = "ZANIME_TEST_DOTENV_VALUE"
	_ = os.Unsetenv(name)
	t.Cleanup(func() { _ = os.Unsetenv(name) })

	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n\n" + name + "=42\nIGNORED LINE\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv(name); got != "42" {
		t.Fatalf("%s = %q, want 42", name, got)
	}
}

func TestLoadDotEnvKeepsExistingValue(t *testing.T) {
	const name = "ZANIME_TEST_DOTENV_KEEP"
	t.Setenv(name, "keep")

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(name+"=new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv(name); got != "keep" {
		t.Fatalf("%s = %q, want keep", name, got)
	}
}

func TestLoadDotEnvMissingFileIsFine(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("missing .env: %v", err)
	}
}

func TestIntFallsBackOnInvalidValue(t *testing.T) {
	t.Setenv("ZANIME_TEST_INT", "nope")
	if got := Int("ZANIME_TEST_INT", 7); got != 7 {
		t.Fatalf("Int = %d, want 7", got)
	}
}
