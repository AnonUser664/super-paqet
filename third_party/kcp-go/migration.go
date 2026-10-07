// File migration.go moves a live conversation between conversation-aware
// sockets. Reliability, timers and stream bytes stay with the same session.
package kcp

import (
	"fmt"
	"net"
	"sync"
)

// migrationMu serializes ownership transfers and session removal, never input
// dispatch. Listener session locks continue to protect ordinary packet lookup.
var migrationMu sync.Mutex

// sessionRoute is an immutable snapshot of physical transport ownership.
// Readers can inspect addresses without racing a source-port change.
type sessionRoute struct {
	conn     net.PacketConn
	remote   net.Addr
	listener *Listener
	platform platform
}

// CanMigrate reports whether both conversations use movable dispatchers.
// Ordinary dedicated readers and address-only/FEC listeners cannot migrate.
func (s *UDPSession) CanMigrate(via *UDPSession) bool {
	if via == nil {
		return false
	}
	a, b := s.route.Load(), via.route.Load()
	return a.listener != nil && b.listener != nil && a.listener.multiConversation && b.listener.multiConversation && !s.isClosed() && !via.isClosed()
}

// MigrateVia adopts the candidate's physical listener and return address. The
// caller must authenticate the move and ensure identical cipher/FEC settings.
// The candidate remains independent and can close after the move is confirmed.
func (s *UDPSession) MigrateVia(via *UDPSession) error {
	if !s.CanMigrate(via) {
		return fmt.Errorf("conversation migration unsupported")
	}
	r := via.route.Load()
	return r.listener.MoveSession(s, r.remote)
}

// MoveSession transfers table ownership before the old socket can close. Queued
// output is retargeted when transmitted, so it cannot retain a stale address.
func (l *Listener) MoveSession(s *UDPSession, remote net.Addr) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()
	old := s.route.Load()
	if old.listener == nil || !old.listener.multiConversation || !l.multiConversation || s.isClosed() || remote == nil {
		return fmt.Errorf("conversation migration unsupported or closed")
	}
	for _, owner := range []*Listener{old.listener, l} {
		select {
		case <-owner.die:
			return net.ErrClosed
		default:
		}
	}
	old.listener.sessionLock.Lock()
	defer old.listener.sessionLock.Unlock()
	if l != old.listener {
		l.sessionLock.Lock()
		defer l.sessionLock.Unlock()
	}
	key := l.conversationKey(remote, s.kcp.conv)
	if other := l.sessions[key]; other != nil && other != s {
		return fmt.Errorf("conversation migration collision")
	}
	if maximum := l.maxSessions.Load(); l != old.listener && maximum > 0 && int64(len(l.sessions)) >= maximum {
		return fmt.Errorf("conversation migration limit reached")
	}
	oldKey := old.listener.conversationKey(old.remote, s.kcp.conv)
	if old.listener.sessions[oldKey] != s {
		return fmt.Errorf("conversation migration lost ownership")
	}
	s.routeMu.Lock()
	delete(old.listener.sessions, oldKey)
	l.sessions[key] = s
	s.route.Store(&sessionRoute{conn: l.conn, remote: remote, listener: l, platform: makePlatform(l.conn)})
	s.routeMu.Unlock()
	return nil
}

// RetransmitNow resumes pending ARQ immediately after a verified physical move,
// retaining sequence numbers and receiver state rather than restarting KCP.
func (s *UDPSession) RetransmitNow() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for seg := range s.kcp.snd_buf.ForEach {
		seg.resendts = currentMs()
		seg.rto = s.kcp.rx_rto
	}
	s.kcp.flush(IKCP_FLUSH_FULL)
}
