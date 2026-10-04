//go:build linux

package engine

import (
	kcplib "github.com/xtaci/kcp-go/v5"
	"math"
	"paqet/internal/tnet/kcp"
	"time"
)

// controller adjusts the in-flight memory budget using delivered bytes and the
// lowest observed RTT. It does not confuse random loss with queue congestion.
// Limits remain explicit ceilings. These heuristics require link-matrix testing.
type controller struct {
	packetRate               float64
	bulkSeen                 bool
	maximum, receive, window int
	previous                 kcplib.TransportStats
	last                     time.Time
	minRTT                   float64
	rate                     float64
	samples                  int
	startup                  bool
	peakRate                 float64
	plateau                  int
	lossRatio                float64
	passive                  bool
	delivery                 [32]deliverySample
	deliveryNext             int
	congested                bool
	slot                     *slot
	hotUntil                 time.Time
}

type deliverySample struct {
	at   time.Time
	rate float64
}

func newController(maximum, receive int) *controller {
	return &controller{maximum: maximum, receive: receive, window: min(4, maximum), startup: true}
}

func (c *controller) update(s kcplib.TransportStats, now time.Time) int {
	if c.last.IsZero() {
		c.previous = s
		c.last = now
		return c.window
	}
	elapsed := now.Sub(c.last).Seconds()
	if elapsed <= 0 {
		return c.window
	}
	acked := s.AckedBytes - c.previous.AckedBytes
	packets := s.AckedSegments - c.previous.AckedSegments
	previousPending := c.previous.Pending
	waited := s.WriteWaitCount > c.previous.WriteWaitCount
	resent := s.RetransmittedSegments - c.previous.RetransmittedSegments
	sent := s.SentSegments - c.previous.SentSegments
	c.previous = s
	c.last = now
	if s.SRTT > 0 && (c.minRTT == 0 || float64(s.SRTT) < c.minRTT) {
		// Opening/keepalive samples reveal propagation delay even before
		// bulk traffic. Do not learn an initial queue-filled RTT as the floor.
		c.minRTT = float64(s.SRTT)
	}
	rtt := math.Max(1, float64(s.SRTT))
	if c.minRTT > 0 {
		if s.TransitSamples > 0 {
			threshold := max(5, max(float64(s.SRTTVar)*2, c.minRTT/8))
			if c.congested {
				threshold = max(3, threshold/2)
			}
			c.congested = float64(s.ForwardQueue) > threshold
		} else if c.congested {
			c.congested = rtt > c.minRTT*1.1+2
		} else {
			c.congested = rtt > c.minRTT*1.25+5
		}
	}
	if acked == 0 {
		return c.window
	}
	if packets > 0 {
		c.packetRate = max(float64(packets)/elapsed, c.packetRate*.75)
		if acked/packets >= uint64(max(1, s.MSS)/4) || s.PendingBytes >= uint64(max(1, s.MSS)*2) {
			c.bulkSeen = true
		}
	} else if acked >= uint64(max(1, s.MSS)*2) {
		c.bulkSeen = true
	}
	// Keepalive/opening traffic must not erase a learned bulk capacity or
	// collapse the initial window before the first application transfer.
	if acked < uint64(max(1, s.MSS)*2) && s.Pending < c.window/2 {
		if !c.bulkSeen && (waited || packets > 0) {
			target := int(math.Ceil(2*c.packetRate*rtt/1000)) + 2
			c.window = min(c.maximum, max(c.window, target))
		}
		return c.window
	}
	if c.minRTT == 0 || rtt < c.minRTT {
		c.minRTT = rtt
	}
	measured := float64(acked) / elapsed
	// Short gaps in ordered delivery are not a new path bandwidth ceiling.
	// Keep a recent delivery maximum over several RTTs; expiry still lets a
	// sustained capacity reduction converge. Queue signals bound the window
	// immediately instead of requiring a static rate configuration.
	c.delivery[c.deliveryNext] = deliverySample{now, measured}
	c.deliveryNext = (c.deliveryNext + 1) % len(c.delivery)
	horizon := max(2*time.Second, min(8*time.Second, time.Duration(c.minRTT*4)*time.Millisecond))
	bandwidth := measured
	for _, sample := range c.delivery {
		if !sample.at.IsZero() && now.Sub(sample.at) <= horizon {
			bandwidth = max(bandwidth, sample.rate)
		}
	}
	previousRate := c.rate
	c.samples++
	if c.rate == 0 {
		c.rate = bandwidth
	} else if bandwidth >= c.rate {
		c.rate = bandwidth
	} else if waited || s.Pending >= c.window/2 || previousPending >= c.window/2 {
		c.rate = .75*c.rate + .25*bandwidth
	}
	mss := max(1, s.MSS)
	// A modest BDP margin sustains delivery without blindly filling queues.
	// RTT includes both directions: shrinking the data window repeatedly in
	// response to a reverse-path queue can lock both senders into a low-rate
	// state. Budget bounded ACK-delay headroom; pacing drains the queue.
	flightRTT := min(rtt, c.minRTT*4)
	target := int(math.Ceil(2*c.rate*flightRTT/1000/float64(mss))) + 2
	target = max(target, int(math.Ceil(2*c.packetRate*flightRTT/1000))+2)
	target = min(c.maximum, max(min(4, c.maximum), target))
	saturated := waited || s.Pending >= c.window/2
	queued := c.congested
	if sent > 0 {
		c.lossRatio = .75*c.lossRatio + .25*min(.5, float64(resent)/float64(sent))
	}
	if c.startup && acked >= uint64(mss*2) {
		if measured > c.peakRate*1.1 {
			c.peakRate = measured
			c.plateau = 0
		} else if saturated {
			c.plateau++
		}
		if queued || c.plateau >= 8 {
			c.startup = false
		}
	}
	// A queue signal suppresses probing; it must not force the window below
	// the measured bandwidth-delay budget on every tick (random loss can
	// inflate SRTT without persistent queue congestion).
	if !queued && saturated {
		// Keep discovered credit while a probe's delivery sample catches up.
		// Shrinking it immediately on the next tick cancels the probe itself.
		target = max(target, c.window)
		if measured > previousRate*1.03 {
			target = max(target, min(c.maximum, c.window+c.window/4+1))
		} else if c.samples%8 == 0 {
			target = max(target, min(c.maximum, c.window+c.window/4+1))
		}
		if c.startup {
			target = max(target, min(c.maximum, c.window*2))
		}
	}
	// Bound both probe growth and reductions to prevent oscillation.
	if target > c.window {
		growth := c.window + c.window/4 + 1
		if c.startup && !queued {
			growth = c.window * 2
		}
		c.window = min(target, growth)
	} else {
		c.window = max(target, c.window*3/4)
	}
	c.window = min(c.maximum, max(1, c.window))
	return c.window
}

func (c *controller) pacingRate() uint64 {
	if c.minRTT < 10 || c.rate <= 0 || !c.bulkSeen {
		return 0
	}
	gain := 1.05 * (1 + c.lossRatio)
	if c.congested {
		gain = .85
	} else if c.startup || c.samples%8 == 0 {
		gain = 2
	}
	return uint64(max(1024, c.rate*gain))
}

func (e *Engine) addTuner(conn *kcp.Conn, maximum, receive int, slots ...*slot) {
	c := newController(maximum, receive)
	if len(slots) > 0 {
		c.slot = slots[0]
	}
	conn.UDPSession.SetWindowSize(c.window, receive)
	e.tuneMu.Lock()
	e.tuners[conn] = c
	e.tuneMu.Unlock()
}

func (e *Engine) addPassive(conn *kcp.Conn) {
	s := conn.UDPSession.TransportStats()
	e.tuneMu.Lock()
	e.tuners[conn] = &controller{window: s.SendWindow, passive: true}
	e.tuneMu.Unlock()
}

func (e *Engine) tune() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case now := <-ticker.C:
			e.tuneMu.Lock()
			for conn, c := range e.tuners {
				if conn.Session.IsClosed() {
					delete(e.tuners, conn)
					continue
				}
				if c.passive {
					continue
				}
				s := conn.UDPSession.TransportStats()
				if c.slot != nil && c.slot.conn.Load() == conn {
					traffic := s.AckedBytes - c.previous.AckedBytes + s.ReceivedBytes - c.previous.ReceivedBytes
					wait := s.WriteWaitNanoseconds - c.previous.WriteWaitNanoseconds
					streams := conn.Session.NumStreams()
					score := uint64(streams)
					if traffic > 8192 || s.PendingBytes >= uint64(max(1, s.MSS)*2) || (traffic > 2048 && wait > uint64(100*time.Millisecond)) {
						c.hotUntil = now.Add(max(2*time.Second, min(10*time.Second, time.Duration(s.SRTT)*4*time.Millisecond)))
					}
					if streams > 0 && now.Before(c.hotUntil) {
						score += busyCarrier
					}
					c.slot.score.Store(score)
				}
				if s.SRTT >= 10 {
					conn.UDPSession.SetACKDelay(time.Duration(min(20, max(1, int(c.minRTT/5)))) * time.Millisecond)
				} else {
					conn.UDPSession.SetACKDelay(0)
				}
				if s.SRTT >= 20 {
					conn.UDPSession.SetReorderGrace(min(50, max(s.ReorderDelay, uint32(max(1, s.SRTTVar*4+1)))))
				} else {
					conn.UDPSession.SetReorderGrace(0)
				}
				// Burst queues can put late segments beyond a single SRTT even
				// on a lossless high-delay path. Fast retransmission still handles
				// explicit gaps; the timer floor avoids racing the normal ACKs.
				conn.UDPSession.SetMinRTO(uint32(max(30, int(s.SRTT)*2+int(s.SRTTVar)*4)))
				window := c.update(s, now)
				conn.UDPSession.SetPacingRate(c.pacingRate())
				if s.SendWindow != window {
					conn.UDPSession.SetWindowSize(window, c.receive)
				}
			}
			e.tuneMu.Unlock()
		}
	}
}
