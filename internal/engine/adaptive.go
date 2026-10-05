//go:build linux

// File adaptive.go: uses per-carrier delivery and queue feedback to bound windows/pacing
// without allocating a controller per customer stream.

package engine

import (
	kcplib "github.com/xtaci/kcp-go/v5"
	"math"
	"paqet/internal/conf"
	"paqet/internal/tnet/kcp"
	"sync/atomic"
	"time"
)

// controller adjusts the in-flight memory budget using delivered bytes and the
// lowest observed RTT. It does not confuse random loss with queue congestion.
// Limits remain explicit ceilings. These heuristics require link-matrix testing.
type controller struct {
	// Forward-attributed queue growth used to distinguish congestion from reverse ACK pressure.
	queueSignal float64
	// Recent delivered segment rate, including small control observations before bulk begins.
	packetRate float64
	// Keeps tiny keepalive/opening traffic from being treated as a fresh bulk capacity estimate.
	bulkSeen bool
	// Configured send ceiling, receive setting and currently selected send window, in segments.
	maximum, receive, window int
	// Resource-owned template identity used for scoped live reliability updates.
	endpoint *atomic.Pointer[Endpoint]
	// Prior observation retained for counter deltas rather than cumulative-rate errors.
	previous kcplib.TransportStats
	// Time of the previous delivery/activity observation.
	last time.Time
	// Observed propagation/ACK-adjusted RTT floor in milliseconds.
	minRTT float64
	// Known ACK/update scheduling headroom when peer timing extensions are off.
	// This prevents local protocol batching being mistaken for network congestion.
	ackScheduleBudget float64
	// Estimated payload delivery rate in bytes per second.
	rate float64
	// Observation count used for bounded probing cadence.
	samples int
	// Allows bounded window/rate growth while learning a new path.
	startup bool
	// Retains peak recent delivery so temporary ordered-delivery gaps do not collapse capacity.
	peakRate float64
	// Counts periods without capacity growth to end aggressive startup probing.
	plateau int
	// Estimated retransmission fraction, separate from attributed queue congestion.
	lossRatio float64
	// Keeps diagnostics registered while disabling controller mutations for static endpoints.
	passive bool
	// Recent timestamped rates, retained only per carrier rather than per application stream.
	delivery [32]deliverySample
	// Next bounded history slot; old rate samples are replaced rather than accumulated.
	deliveryNext int
	// Hysteretic queue classification used to avoid reacting permanently to reorder/jitter
	// noise.
	congested bool
	// Optional outgoing pool member receiving cached pressure scores from this controller.
	slot *slot
	// Keeps a carrier's cached busy score alive briefly after a bulk observation.
	hotUntil time.Time
}

// deliverySample retains a timestamped delivery rate so one short delivery gap does not erase
// a learned path capacity.
type deliverySample struct {
	// Timestamp for delta/rate expiry calculations.
	at time.Time
	// Estimated payload delivery rate in bytes per second.
	rate float64
}

// newController starts with a small in-flight budget so startup probes do not allocate or
// flood the full configured ceiling.
func newController(maximum, receive int) *controller {
	return &controller{maximum: maximum, receive: receive, window: min(4, maximum), startup: true}
}

// update updates delivery/window estimates while separating idle control traffic, noisy RTT
// and sustained capacity changes.
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
	networkRTT := math.Max(1, float64(s.SRTT)-float64(s.PeerACKDelay))
	if s.SRTT > 0 && (c.minRTT == 0 || networkRTT < c.minRTT) {
		// Opening/keepalive samples reveal propagation delay even before
		// bulk traffic. Do not learn an initial queue-filled RTT as the floor.
		c.minRTT = networkRTT
	}
	rtt := math.Max(1, float64(s.SRTT))
	if c.minRTT > 0 {
		if s.TransitSamples > 0 {
			threshold := max(5, max(float64(s.SRTTVar)*4, c.minRTT/4))
			if c.congested {
				threshold = max(3, threshold*.9)
			}
			// Transit minima can be extreme jitter/reorder samples. Use them
			// to attribute RTT growth, rather than treating their full spread
			// as additional queue delay on an otherwise unchanged RTT.
			total := float64(s.ForwardQueue) + float64(s.ReverseQueue)
			c.queueSignal = max(0, networkRTT-c.minRTT) * float64(s.ForwardQueue) / max(1, total)
			c.congested = c.queueSignal > threshold
		} else if c.congested {
			c.queueSignal = max(0, rtt-c.minRTT-c.ackScheduleBudget)
			c.congested = rtt > c.minRTT*1.1+2+c.ackScheduleBudget
		} else {
			c.queueSignal = max(0, rtt-c.minRTT-c.ackScheduleBudget)
			c.congested = rtt > c.minRTT*1.25+5+c.ackScheduleBudget
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

// pacingRate chooses a bounded send rate from delivery and queue pressure; exempt controls can
// still release backpressure.
func (c *controller) pacingRate() uint64 {
	if c.minRTT < 10 || c.rate <= 0 || !c.bulkSeen {
		return 0
	}
	gain := 1.05 * (1 + c.lossRatio)
	if c.congested {
		// Drain a large real queue promptly; a fixed gentle reduction can
		// leave a narrow uplink saturated by data plus ACK/control traffic.
		gain = max(.25, min(.85, c.minRTT/(c.minRTT+c.queueSignal)))
	} else if c.startup || c.samples%8 == 0 {
		gain = 2
	}
	return uint64(max(1024, c.rate*gain))
}

// addTuner registers adaptive carrier state under the tuner lock and installs its initial
// receive/send limits.
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

// addPassive registers telemetry without enabling adaptive changes, preserving explicit static
// endpoint behavior.
func (e *Engine) addPassive(conn *kcp.Conn) {
	s := conn.UDPSession.TransportStats()
	e.tuneMu.Lock()
	e.tuners[conn] = &controller{window: s.SendWindow, passive: true}
	e.tuneMu.Unlock()
}

// tune samples every carrier on one shared ticker and applies bounded
// pacing/window/ACK/reordering/timer adjustments.
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
				if c.passive {
					// Static reliability still needs coherent pressure for pool
					// balancing/growth; cumulative counters are never interval rates.
					c.previous, c.last = s, now
					continue
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

// addEndpoint registers a carrier and reapplies the latest resource template
// under the reload/tuner synchronization boundary. This closes the race between
// a socket opening with old settings and a concurrent live edit committing.
func (e *Engine) addEndpoint(conn *kcp.Conn, endpoint *atomic.Pointer[Endpoint], slots ...*slot) {
	e.tuneMu.Lock()
	defer e.tuneMu.Unlock()
	cfg := endpoint.Load()
	kcp.ReconfigureReliability(conn.UDPSession, &cfg.KCP)
	var c *controller
	if cfg.Adaptive == nil || *cfg.Adaptive {
		c = newController(cfg.KCP.Sndwnd, cfg.KCP.Rcvwnd)
		c.ackScheduleBudget = reliabilityACKBudget(cfg.KCP)
		conn.UDPSession.SetWindowSize(c.window, c.receive)
	} else {
		s := conn.UDPSession.TransportStats()
		c = &controller{window: s.SendWindow, passive: true}
	}
	if len(slots) > 0 {
		c.slot = slots[0]
	}
	c.endpoint = endpoint
	e.tuners[conn] = c
}

// reliabilityACKBudget resolves preset/manual ACK scheduling. Without peer
// timestamps, RTT growth below this known bound cannot identify link queueing.
// An immediate-ACK mode can still defer to the adaptive ACK-delay limit.
func reliabilityACKBudget(cfg conf.KCP) float64 {
	interval, immediate := cfg.Interval, cfg.AckNoDelay
	switch cfg.Mode {
	case "normal":
		interval, immediate = 40, false
	case "fast":
		interval, immediate = 30, false
	case "fast2":
		interval, immediate = 20, true
	case "fast3":
		interval, immediate = 10, true
	}
	budget := cfg.ACKDelayMaxMS
	if !immediate {
		budget += interval
	}
	return float64(budget)
}
