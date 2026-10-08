// File conversation_socket.go supports independent KCP conversations on one
// fixed raw/UDP tuple without changing the existing packet encoding.
package kcp

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
)

// conversationKey keeps the existing endpoint identity and adds the inner KCP
// conversation only for explicitly enabled shared endpoints.
type conversationKey struct {
	addr [16]byte
	port uint16
	str  string
	conv uint32
}

// outgoingHint bypasses address-string allocation and the map lock for the
// usual one-conversation source socket. The complete map remains authoritative
// for other conversations; route ownership is rechecked on every hint hit.
type outgoingHint struct {
	session *UDPSession
	remote  *net.UDPAddr
}

// hintOutgoing changes only an outgoing dispatch optimization, never ownership.
func (l *Listener) hintOutgoing(s *UDPSession, remote net.Addr) {
	if !l.dialOnly {
		return
	}
	if udp, ok := remote.(*net.UDPAddr); ok {
		l.outgoing.Store(&outgoingHint{session: s, remote: udp})
	}
}

// clearOutgoing prevents a retired hint from retaining closed protocol buffers.
func (l *Listener) clearOutgoing(s *UDPSession) {
	if hint := l.outgoing.Load(); hint != nil && hint.session == s {
		l.outgoing.CompareAndSwap(hint, nil)
	}
}

// conversationKey leaves ordinary ServeConn's reset-by-address semantics intact.
func (l *Listener) conversationKey(remote net.Addr, conv uint32) conversationKey {
	if !l.multiConversation {
		conv = 0
	}
	if udp, ok := remote.(*net.UDPAddr); ok && udp != nil && udp.IP.To16() != nil && udp.Port >= 0 && udp.Port <= 65535 {
		var k conversationKey
		if ip16 := udp.IP.To16(); ip16 != nil {
			copy(k.addr[:], ip16)
		}
		k.port = uint16(udp.Port)
		k.conv = conv
		if udp.Zone != "" {
			k.str = udp.Zone
		}
		return k
	}
	if remote != nil {
		return conversationKey{str: remote.String(), conv: conv}
	}
	return conversationKey{conv: conv}
}

// ServeConversationConn routes by endpoint and conversation. The caller owns
// the packet socket. Legacy parity-only FEC frames cannot identify a conversation
// and are deliberately rejected rather than silently routed to the wrong lane.
func ServeConversationConn(block BlockCrypt, dataShards, parityShards int, conn net.PacketConn, dialOnly bool) (*Listener, error) {
	if dataShards != 0 || parityShards != 0 {
		return nil, fmt.Errorf("shared conversations require FEC disabled")
	}
	return serveConversationConn(block, 0, 0, conn, false, true, dialOnly)
}

// DialConversation registers an outgoing lane before any packet can be sent.
// One receive loop decrypts and dispatches all lanes; each lane retains its own
// reliability, send queue, pacing and lifecycle. Unknown input creates no state.
func (l *Listener) DialConversation(remote net.Addr) (*UDPSession, error) {
	if !l.multiConversation || !l.dialOnly {
		return nil, fmt.Errorf("not a shared outgoing socket")
	}
	l.sessionLock.Lock()
	defer l.sessionLock.Unlock()
	select {
	case <-l.die:
		return nil, net.ErrClosed
	default:
	}
	if maximum := l.maxSessions.Load(); maximum > 0 && int64(len(l.sessions)) >= maximum {
		return nil, fmt.Errorf("shared conversation limit reached")
	}
	for {
		var raw [4]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, err
		}
		conv := binary.LittleEndian.Uint32(raw[:])
		key := l.conversationKey(remote, conv)
		if _, exists := l.sessions[key]; exists {
			continue
		}
		s := newUDPSession(conv, 0, 0, l, l.conn, false, remote, l.block)
		l.sessions[key] = s
		l.hintOutgoing(s, remote)
		return s, nil
	}
}
