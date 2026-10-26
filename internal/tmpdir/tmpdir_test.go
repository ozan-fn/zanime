package tmpdir

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func reset() {
	once = sync.Once{}
	base = ""
}

func TestBaseUsesEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZANIME_TMP_DIR", dir)
	if got := Base(); got != dir {
		t.Fatalf("Base() = %q, want %q", got, dir)
	}
}

// ./tmp didahulukan supaya data tidak hilang saat container restart (Pterodactyl).
func TestBasePrefersLocalTmp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZANIME_TMP_DIR", "")
	t.Chdir(dir)
	reset()

	want := filepath.Join(dir, "tmp")
	if got := Base(); got != want {
		t.Fatalf("Base() = %q, want %q", got, want)
	}
	if st, err := os.Stat(want); err != nil || !st.IsDir() {
		t.Fatalf("folder ./tmp tidak dibuat: %v", err)
	}
}

// cwd read-only (./tmp tidak bisa dibuat) → pakai temp sistem.
func TestBaseFallsBackToOSTemp(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "tmp") // file bernama "tmp" menghalangi MkdirAll
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZANIME_TMP_DIR", "")
	t.Chdir(dir)
	reset()

	if got, want := Base(), os.TempDir(); got != want {
		t.Fatalf("Base() = %q, want %q", got, want)
	}
}
