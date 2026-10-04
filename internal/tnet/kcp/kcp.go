package kcp

import (
	"fmt"
	"paqet/internal/conf"
	"time"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

func aplConf(conn *kcp.UDPSession, cfg *conf.KCP) error {
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
	conn.SetWindowSize(cfg.Sndwnd, cfg.Rcvwnd)
	if !conn.SetMtu(cfg.MTU) {
		return fmt.Errorf("KCP MTU %d cannot fit transport overhead", cfg.MTU)
	}
	conn.SetWriteDelay(wDelay)
	conn.SetACKNoDelay(ackNoDelay)
	conn.SetACKTimestamps(cfg.ACKTimestamps == nil || *cfg.ACKTimestamps)
	conn.SetStreamMode(true)
	conn.SetDSCP(46)
	return nil
}

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
