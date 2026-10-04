//go:build linux

package engine

import (
	kcp "github.com/xtaci/kcp-go/v5"
	"testing"
	"time"
)

func TestAdaptiveBoundsAndQueueResponse(t *testing.T) {
	c := newController(1024, 4096)
	now := time.Now()
	s := kcp.TransportStats{SRTT: 20, MSS: 1306, Pending: 10000}
	c.update(s, now)
	for i := 0; i < 20; i++ {
		s.AckedBytes += 1000000
		now = now.Add(250 * time.Millisecond)
		w := c.update(s, now)
		if w < 1 || w > 1024 {
			t.Fatal("window exceeded ceiling")
		}
	}
	before := c.pacingRate()
	s.SRTT = 200
	s.AckedBytes += 1000000
	now = now.Add(250 * time.Millisecond)
	c.update(s, now)
	if after := c.pacingRate(); !c.congested || after >= before {
		t.Fatal("queue growth did not back off pacing")
	}
}

func TestAdaptiveLowBandwidthAndIdle(t *testing.T) {
	c := newController(4096, 4096)
	now := time.Now()
	s := kcp.TransportStats{SRTT: 100, MSS: 1306}
	c.update(s, now)
	for i := 0; i < 30; i++ {
		s.AckedBytes += 3125
		now = now.Add(250 * time.Millisecond)
		c.update(s, now)
	}
	if c.window > 32 {
		t.Fatalf("low-bandwidth window too large: %d", c.window)
	}
	before := c.window
	now = now.Add(time.Minute)
	if c.update(s, now) != before {
		t.Fatal("idle changed the window without measurements")
	}
}

func TestControlTrafficPreservesLearnedCapacity(t *testing.T) {
	c := newController(32768, 32768)
	now := time.Now()
	s := kcp.TransportStats{SRTT: 100, MSS: 1306, Pending: 10000}
	c.update(s, now)
	for i := 0; i < 8; i++ {
		now = now.Add(250 * time.Millisecond)
		s.AckedBytes += 4000000
		c.update(s, now)
	}
	window, rate := c.window, c.rate
	s.Pending = 0
	for i := 0; i < 40; i++ {
		now = now.Add(250 * time.Millisecond)
		s.AckedBytes += 8
		c.update(s, now)
	}
	if c.window != window || c.rate != rate {
		t.Fatal("keepalive traffic erased bulk estimate")
	}
}

func TestDeliveryGapDoesNotEraseCapacityAndSustainedDropConverges(t *testing.T) {
	c := newController(32768, 32768)
	now := time.Unix(100, 0)
	s := kcp.TransportStats{SRTT: 80, MSS: 1306, Pending: 10000}
	c.update(s, now)
	now = now.Add(250 * time.Millisecond)
	s.AckedBytes += 1000000
	c.update(s, now)
	rate := c.rate
	for i := 0; i < 6; i++ {
		now = now.Add(250 * time.Millisecond)
		s.AckedBytes += 10000 // Loss recovery temporarily stalls ordered drain.
		c.update(s, now)
		if c.rate < rate {
			t.Fatal("transient delivery gap collapsed bandwidth estimate")
		}
	}
	for i := 0; i < 40; i++ {
		now = now.Add(250 * time.Millisecond)
		s.AckedBytes += 10000
		c.update(s, now)
	}
	if c.rate > 50000 {
		t.Fatalf("sustained bottleneck did not converge: %f", c.rate)
	}
}

func TestControlSamplesLearnRTTBeforeBulkWithoutGrowingWindow(t *testing.T) {
	c := newController(32768, 32768)
	now := time.Unix(100, 0)
	s := kcp.TransportStats{SRTT: 50, MSS: 1306}
	c.update(s, now)
	s.AckedBytes = 64
	c.update(s, now.Add(250*time.Millisecond))
	if c.minRTT != 50 || c.rate != 0 || c.window != 4 {
		t.Fatalf("control samples did not preserve conservative startup: %+v", c)
	}
	s.SRTT = 1000
	s.AckedBytes += 10000
	s.Pending = 100
	c.update(s, now.Add(500*time.Millisecond))
	if c.minRTT != 50 || !c.congested || c.pacingRate() >= uint64(c.rate) {
		t.Fatal("initial queue mistaken for path RTT")
	}
}

func TestReverseQueueDoesNotThrottleForwardPacing(t *testing.T) {
	c := newController(32768, 32768)
	now := time.Unix(100, 0)
	s := kcp.TransportStats{SRTT: 50, MSS: 1306, TransitSamples: 1}
	c.update(s, now)
	now = now.Add(250 * time.Millisecond)
	s.AckedBytes = 1000000
	c.update(s, now)
	s.SRTT, s.ForwardQueue, s.ReverseQueue = 250, 0, 200
	now = now.Add(250 * time.Millisecond)
	s.AckedBytes += 1000000
	c.update(s, now)
	if c.congested || c.pacingRate() < uint64(c.rate) {
		t.Fatal("reverse ACK queue throttled fast sending direction")
	}
	s.ForwardQueue = 100
	now = now.Add(250 * time.Millisecond)
	s.AckedBytes += 1000000
	c.update(s, now)
	if !c.congested || c.pacingRate() >= uint64(c.rate) {
		t.Fatal("forward queue did not reduce paced data")
	}
}

func TestSmallControlPacketRateGrowsCreditWithoutDataPacing(t *testing.T) {
	c := newController(1024, 4096)
	now := time.Unix(100, 0)
	s := kcp.TransportStats{SRTT: 100, MSS: 1306}
	c.update(s, now)
	for i := 0; i < 10; i++ {
		now = now.Add(250 * time.Millisecond)
		s.AckedBytes += 1000
		s.AckedSegments += 50
		s.WriteWaitCount++
		c.update(s, now)
	}
	if c.window < 40 || c.pacingRate() != 0 {
		t.Fatal("small control traffic was byte-limited or paced", c.window, c.pacingRate())
	}
}
