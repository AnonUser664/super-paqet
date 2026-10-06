// File kcp.go: maps presets/manual knobs to KCP and enables the enterprise mux extensions
// inside the raw envelope.

package kcp

import (
	"fmt"
	"paqet/internal/conf"
	"time"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

// aplConf maps preset or manual parameters to KCP and configures the retained
// reliability/ACK/packet-size behavior.
func aplConf(conn *kcp.UDPSession, cfg *conf.KCP) error {
	ReconfigureReliability(conn, cfg)
	conn.SetWindowSize(cfg.Sndwnd, cfg.Rcvwnd)
	if !conn.SetMtu(cfg.MTU) {
		return fmt.Errorf("KCP MTU %d cannot fit transport overhead", cfg.MTU)
	}
	conn.SetACKTimestamps(cfg.ACKTimestamps == nil || *cfg.ACKTimestamps)
	conn.SetStreamMode(true)
	conn.SetDSCP(46)
	return nil
}

// smuxConf enables enterprise half-close, credit/priority and buffer ceilings above the same
// reliable KCP carrier.
func smuxConf(cfg *conf.KCP, conn *kcp.UDPSession) *smux.Config {
	var sconf = smux.DefaultConfig()
	sconf.HalfClose = cfg.HalfClose
	sconf.AsyncWindowUpdates = true
	sconf.TransportWriteLimit = conn.WriteBudget
	sconf.PrioritizeControl = true
	sconf.CreditHints = cfg.CreditHints == nil || *cfg.CreditHints
	sconf.AdaptiveReceive = cfg.AdaptiveBuffers
	sconf.TransportRTT = func() time.Duration { return time.Duration(conn.GetSRTT()) * time.Millisecond }
	sconf.Version = 2
	sconf.KeepAliveInterval = cfg.Smuxkalive
	sconf.KeepAliveTimeout = cfg.Smuxktimeout
	sconf.MaxFrameSize = 65535
	sconf.MaxReceiveBuffer = cfg.Smuxbuf
	sconf.MaxStreamBuffer = cfg.Streambuf
	return sconf
}

// ReconfigureReliability applies only lock-protected scheduling/retransmission
// settings. It leaves MTU, queued segment encoding, cipher/FEC, windows and mux
// contracts intact, so established streams survive these live edits.
func ReconfigureReliability(conn *kcp.UDPSession, cfg *conf.KCP) {
	var noDelay, interval, resend, noCongestion int
	var wDelay, ackNoDelay bool
	switch cfg.Mode {
	case "normal":
		noDelay, interval, resend, noCongestion = 0, 40, 2, 1
		wDelay, ackNoDelay = true, false
	case "fast":
		noDelay, interval, resend, noCongestion = 0, 30, 2, 1
		wDelay, ackNoDelay = true, false
	case "fast2":
		noDelay, interval, resend, noCongestion = 1, 20, 2, 1
		wDelay, ackNoDelay = false, true
	case "fast3":
		noDelay, interval, resend, noCongestion = 1, 10, 2, 1
		wDelay, ackNoDelay = false, true
	case "manual":
		noDelay, interval, resend, noCongestion = cfg.NoDelay, cfg.Interval, cfg.Resend, cfg.NoCongestion
		wDelay, ackNoDelay = cfg.WDelay, cfg.AckNoDelay
	}

	conn.SetNoDelay(noDelay, interval, resend, noCongestion)
	conn.SetWriteDelay(wDelay)
	conn.SetACKNoDelay(ackNoDelay)
	conn.SetACKDelayLimit(time.Duration(cfg.ACKDelayMaxMS) * time.Millisecond)
	conn.SetWriteBatchBudget(uint32(cfg.WriteBatchMS))
	conn.SetSmallWriteFlush(cfg.SmallWriteFlush)
}
