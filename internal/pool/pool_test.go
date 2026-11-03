package pool

import (
	"sync/atomic"
	"testing"
)

func TestGetDefaultsTo20AndRunsTasks(t *testing.T) {
	t.Setenv("WORKERS", "")
	if got := Workers(); got != 20 {
		t.Fatalf("Workers = %d, want 20", got)
	}
	var n atomic.Int64
	group := Get().NewGroup()
	for range 100 {
		group.Submit(func() { n.Add(1) })
	}
	if err := group.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := n.Load(); got != 100 {
		t.Fatalf("tasks = %d, want 100", got)
	}
}
