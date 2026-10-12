// File latency_histogram_test.go protects quantile rank and upper bounds across
// fine/wide tiers without allowing overlapping bins to count requests twice.
package main

import (
	"sort"
	"sync"
	"testing"
)

// Short local requests must not round an entire one-millisecond interval up.
func TestLatencyHistogramShortRequestResolution(t *testing.T) {
	var h latencyHistogram
	for i := 0; i < 100; i++ {
		h.record(1001)
	}
	if upper := h.percentile(100, .99); upper != 1125 {
		t.Fatalf("upper=%d, want 1125 us for a 1001 us request", upper)
	}
}

// Quantiles must still enclose the ranked real observation at every tier edge.
func TestLatencyHistogramTierBoundaries(t *testing.T) {
	values := []int64{0, 124, 125, 999, 1000, 31999, 32000, 32999, 33000, 29999999, 30000000, 31000000}
	var h latencyHistogram
	for _, us := range values {
		h.record(us)
	}
	for _, fraction := range []float64{0, .25, .5, .75, .9, .99} {
		actual := values[int(float64(len(values))*fraction)]
		upper := h.percentile(int64(len(values)), fraction)
		if upper <= actual {
			t.Fatalf("q=%v upper=%d does not bound actual=%d", fraction, upper, actual)
		}
		if actual < 32000 && upper-actual > 125 {
			t.Fatalf("fine bound too coarse: actual=%d upper=%d", actual, upper)
		}
		if actual >= 32000 && actual < 30000000 && upper-actual > 1000 {
			t.Fatalf("wide bound too coarse: actual=%d upper=%d", actual, upper)
		}
	}
	var empty latencyHistogram
	if empty.percentile(0, .99) != 0 {
		t.Fatal("empty histogram fabricated a latency")
	}
}

// Crossing 32 ms must neither re-count the fine tier nor hide wide outliers.
func TestLatencyHistogramTierPopulationDoesNotOverlap(t *testing.T) {
	var h latencyHistogram
	for i := 0; i < 100; i++ {
		h.record(31999)
	}
	for i := 0; i < 5; i++ {
		h.record(32000)
	}
	if h.percentile(105, .95) != 32000 || h.percentile(105, .96) != 33000 {
		t.Fatal("fine/wide boundary changed quantile population")
	}
}

// Concurrent workers share histogram counters; order must not lose samples or
// alter the ranked population observed after all workers have finished.
func TestLatencyHistogramConcurrentWorkers(t *testing.T) {
	var h latencyHistogram
	var wg sync.WaitGroup
	values := make([]int64, 0, 8000)
	for worker := 0; worker < 8; worker++ {
		us := int64(worker+1) * 1000
		for i := 0; i < 1000; i++ {
			values = append(values, us)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				h.record(us)
			}
		}()
	}
	wg.Wait()
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	for _, fraction := range []float64{.5, .99} {
		actual := values[int(float64(len(values))*fraction)]
		if upper := h.percentile(int64(len(values)), fraction); upper <= actual || upper-actual > 125 {
			t.Fatalf("q=%v actual=%d upper=%d", fraction, actual, upper)
		}
	}
}
