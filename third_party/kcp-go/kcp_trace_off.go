//go:build !debug

// if build tag debug is not set, debugLog is a no-op eliminated at compile time
// File kcp_trace_off.go: keeps protocol tracing a no-op in normal builds without changing
// transport state.

package kcp

// debugLog routes protocol trace records only when the build's trace implementation enables
// them.
func (kcp *KCP) debugLog(logtype KCPLogType, args ...any) {}
