// File latency_histogram.go keeps bounded, concurrent benchmark quantiles with
// enough low-latency resolution to compare local mixed-load candidates.
package main

import "sync/atomic"

const (
	// Short requests need finer bounds than the previous one-millisecond bins.
	latencyFineStepUS = 125
	latencyFineMaxUS  = 32000
)

// latencyHistogram retains the existing millisecond and logarithmic ranges.
// The fine tier adds only two KiB and never retains individual observations.
type latencyHistogram struct {
	fine         [latencyFineMaxUS / latencyFineStepUS]atomic.Int64
	milliseconds [30001]atomic.Int64
	logarithmic  [32]atomic.Int64
}

// record adds one successful, measured request; failures/warmup remain excluded
// by the caller. It records the wide tiers too for the legacy long-tail fallback.
func (h *latencyHistogram) record(us int64) {
	if us < latencyFineMaxUS {
		h.fine[us/latencyFineStepUS].Add(1)
	}
	h.milliseconds[min(int(us/1000), len(h.milliseconds)-1)].Add(1)
	bucket := 0
	for us > 1 && bucket < len(h.logarithmic)-1 {
		us >>= 1
		bucket++
	}
	h.logarithmic[bucket].Add(1)
}

// percentile reports an upper bucket bound using the existing quantile rank.
// Skip overlapping millisecond bins after counting fine observations. Beyond
// thirty seconds, restart with the complete logarithmic population so a bucket
// straddling that boundary cannot double-count or omit short observations.
func (h *latencyHistogram) percentile(total int64, fraction float64) int64 {
	threshold := int64(float64(total) * fraction)
	var n int64
	for i := range h.fine {
		n += h.fine[i].Load()
		if n > threshold {
			return int64(i+1) * latencyFineStepUS
		}
	}
	for i := latencyFineMaxUS / 1000; i < len(h.milliseconds)-1; i++ {
		n += h.milliseconds[i].Load()
		if n > threshold {
			return int64(i+1) * 1000
		}
	}
	n = 0
	for i := range h.logarithmic {
		n += h.logarithmic[i].Load()
		if n > threshold {
			return 1 << (i + 1)
		}
	}
	return 0
}
