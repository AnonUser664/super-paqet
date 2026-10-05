// File admission.go: limits unknown conversation allocation before acceptance to bound
// listener resource use.

package kcp

// SetMaxSessions limits allocation of session state before application Accept.
// Zero leaves upstream behavior unchanged. Existing sessions are not evicted.
func (l *Listener) SetMaxSessions(maximum int) { l.maxSessions.Store(int64(maximum)) }
