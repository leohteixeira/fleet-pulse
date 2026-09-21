package httpapi

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriteGuards_IntervalExpiresAfter20s(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	g := newWriteGuards()
	g.now = func() time.Time { return now }

	if _, _, rej := g.reserveInterval("c1"); rej != nil {
		t.Fatalf("first reserve: %+v", rej)
	}

	now = now.Add(20*time.Second - time.Millisecond)
	if _, _, rej := g.reserveInterval("c1"); rej == nil {
		t.Fatal("want 409 at 19.999s")
	}

	now = now.Add(time.Millisecond)
	if _, _, rej := g.reserveInterval("c1"); rej != nil {
		t.Fatalf("want allow at 20s, got %+v", rej)
	}
}

func TestWriteGuards_ReserveIntervalSerializes(t *testing.T) {
	t.Parallel()

	g := newWriteGuards()
	var allowed atomic.Int32
	var rejected atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, rej := g.reserveInterval("same"); rej != nil {
				rejected.Add(1)
				return
			}
			allowed.Add(1)
		}()
	}
	wg.Wait()
	if allowed.Load() != 1 || rejected.Load() != 7 {
		t.Fatalf("allowed=%d rejected=%d, want 1 and 7", allowed.Load(), rejected.Load())
	}
}
