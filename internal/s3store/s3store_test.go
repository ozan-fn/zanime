package s3store

import (
	"strings"
	"testing"
	"time"
)

func at(hour int) time.Time {
	return time.Date(2026, 10, 1, hour, 0, 0, 0, time.UTC)
}

func TestEvictionsDeletesOldestUntilItFits(t *testing.T) {
	objects := []object{
		{key: "zanime/new.mkv", size: 40, modified: at(3)},
		{key: "zanime/old.mkv", size: 30, modified: at(1)},
		{key: "zanime/mid.mkv", size: 30, modified: at(2)},
	}
	drop, err := evictions(objects, 100, 20, 100, nil)
	if err != nil {
		t.Fatalf("evictions: %v", err)
	}
	if len(drop) != 1 || drop[0].key != "zanime/old.mkv" {
		t.Fatalf("drop = %+v, want only the oldest object", drop)
	}
}

func TestEvictionsKeepsPlayingObject(t *testing.T) {
	objects := []object{
		{key: "zanime/playing.mkv", size: 60, modified: at(1)},
		{key: "zanime/other.mkv", size: 60, modified: at(2)},
	}
	drop, err := evictions(objects, 120, 20, 100, map[string]bool{"zanime/playing.mkv": true})
	if err != nil {
		t.Fatalf("evictions: %v", err)
	}
	if len(drop) != 1 || drop[0].key != "zanime/other.mkv" {
		t.Fatalf("drop = %+v, want the unprotected object", drop)
	}
}

func TestEvictionsRejectsFileLargerThanLimit(t *testing.T) {
	_, err := evictions(nil, 0, 101, 100, nil)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v, want size error", err)
	}
}

func TestEvictionsFailsWhenEverythingIsKept(t *testing.T) {
	objects := []object{{key: "zanime/playing.mkv", size: 90, modified: at(1)}}
	_, err := evictions(objects, 90, 20, 100, map[string]bool{"zanime/playing.mkv": true})
	if err == nil || !strings.Contains(err.Error(), "full") {
		t.Fatalf("err = %v, want full-limit error", err)
	}
}

func TestKeyStaysInsideItsPrefix(t *testing.T) {
	s := New(nil, "bucket", "zanime", 0)
	got := s.Key("hash", "../../other/episode.mkv")
	if got != "zanime/hash/other/episode.mkv" {
		t.Fatalf("key = %q, want traversal removed", got)
	}
}
