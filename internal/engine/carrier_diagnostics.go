//go:build linux

// File carrier_diagnostics.go reports one terminal cause per registered carrier.
// Cleanup and idle retirement stay quiet; transport/expiry evidence stays visible.
package engine

import (
	"paqet/internal/tnet/kcp"
)

// logCarrierEnd is called when the shared tuner removes a closed carrier. This
// bounds logging by carrier lifetime rather than customer connection count.
func (e *Engine) logCarrierEnd(c *kcp.Conn) {
	cause := c.Session.EndCause()
	if e.ctx.Err() != nil || cause.Reason == "local_close" || cause.Reason == "idle_retired" {
		return
	}
	e.log().Warn("session.closed", "conv", c.UDPSession.GetConv(), "remote", c.RemoteAddr().String(), "reason", cause.Reason, "error", cause.Error)
}
