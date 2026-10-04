//go:build linux

package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type poolGuard struct{ closed atomic.Int32 }

func (g *poolGuard) Close() error { g.closed.Add(1); return nil }

func TestConcurrentPoolGrowthCreatesOneSpareAndCleansIt(t *testing.T) {
	base := &slot{}
	base.score.Store(busyCarrier)
	ctx := context.Background()
	p := &peer{engine: &Engine{ctx: ctx}, endpoint: Endpoint{MaxSessions: 2}, slots: []*slot{base}}
	var creates atomic.Int32
	guard := &poolGuard{}
	spare := &slot{guard: guard}
	p.createSlot = func(context.Context) (*slot, error) { creates.Add(1); return spare, nil }
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := p.selectSlot(ctx)
			if err != nil || s != spare {
				t.Errorf("spare selection: %v", err)
			}
		}()
	}
	wg.Wait()
	if creates.Load() != 1 || len(p.slots) != 2 {
		t.Fatal("concurrent growth exceeded pool ceiling")
	}
	spare.score.Store(busyCarrier)
	p.selectSlot(ctx)
	if creates.Load() != 1 {
		t.Fatal("hard pool maximum ignored")
	}
	p.close()
	if guard.closed.Load() != 1 {
		t.Fatal("elastic kernel guard leaked")
	}
	if _, err := p.selectSlot(ctx); !errors.Is(err, context.Canceled) && err == nil {
		t.Fatal("closed pool admitted connection")
	}
}

func TestPoolGrowthFailureUsesExistingCarrierAndBacksOff(t *testing.T) {
	base := &slot{}
	base.score.Store(busyCarrier)
	ctx := context.Background()
	p := &peer{engine: &Engine{ctx: ctx}, endpoint: Endpoint{MaxSessions: 2}, slots: []*slot{base}}
	var creates int
	p.createSlot = func(context.Context) (*slot, error) { creates++; return nil, errors.New("test resource pressure") }
	for i := 0; i < 10; i++ {
		if s, err := p.selectSlot(ctx); s != base || err != nil {
			t.Fatal("growth failure interrupted existing carrier")
		}
	}
	if creates != 1 {
		t.Fatal("failed expansion retried without backoff")
	}
}

func TestPoolCloseDuringGrowthClosesUnpublishedGuard(t *testing.T) {
	base := &slot{}
	base.score.Store(busyCarrier)
	ctx := context.Background()
	p := &peer{engine: &Engine{ctx: ctx}, endpoint: Endpoint{MaxSessions: 2}, slots: []*slot{base}}
	entered, release := make(chan struct{}), make(chan struct{})
	guard := &poolGuard{}
	p.createSlot = func(context.Context) (*slot, error) { close(entered); <-release; return &slot{guard: guard}, nil }
	done := make(chan error, 1)
	go func() { _, err := p.selectSlot(ctx); done <- err }()
	<-entered
	p.close()
	close(release)
	if err := <-done; err == nil {
		t.Fatal("shutdown published new carrier")
	}
	if guard.closed.Load() != 1 || len(p.slots) != 1 {
		t.Fatal("shutdown leaked unpublished kernel guard")
	}
}

func TestOpeningRetrySkipsFailedLowPressureCarrier(t *testing.T) {
	failed, healthy := &slot{}, &slot{}
	healthy.score.Store(busyCarrier)
	p := &peer{engine: &Engine{ctx: context.Background()}, endpoint: Endpoint{MaxSessions: 2}, slots: []*slot{failed, healthy}}
	s, err := p.selectSlot(context.Background(), map[*slot]bool{failed: true})
	if err != nil || s != healthy {
		t.Fatal("retry selected failed empty carrier again")
	}
}
