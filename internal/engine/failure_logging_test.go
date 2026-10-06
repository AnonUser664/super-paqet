//go:build linux

// File failure_logging_test.go ensures warn-level deployments retain later
// incident causes without allowing a concurrent error storm to flood logs.
package engine

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLaterFailureWarningIsBoundedAndResumes checks both incident visibility and
// concurrent warning admission with a deterministic clock instead of sleeps.
func TestLaterFailureWarningIsBoundedAndResumes(t *testing.T) {
	e := &Engine{}
	start := int64(time.Minute)
	for n := int64(1); n <= 5; n++ {
		if !e.warnFailure(n, start) {
			t.Fatal("initial cause suppressed")
		}
	}
	if e.warnFailure(6, start+int64(time.Second)) {
		t.Fatal("error burst flooded warning output")
	}
	var wg sync.WaitGroup
	var admitted atomic.Int32
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.warnFailure(100, start+int64(11*time.Second)) {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("concurrent warnings: %d", admitted.Load())
	}
	if !e.warnFailure(200, start+int64(22*time.Second)) {
		t.Fatal("later incident remains silent")
	}
}
