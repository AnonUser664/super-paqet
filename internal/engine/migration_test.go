//go:build linux

// File migration_test.go exercises capability scope, replay rejection and
// logical session identity independently of root-only raw packet fixtures.
package engine

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	kcplib "github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"paqet/internal/tnet/kcp"
)

// migrationIdentity records encoder ownership; unused listener operations are
// deliberately inherited from a nil interface so unexpected calls fail tests.
type migrationIdentity struct {
	tnet.Listener
	added, removed []string
}

// RegisterClient records the adopted tuple without discarding its flag binding.
func (l *migrationIdentity) RegisterClient(a net.Addr, owner uint32) {
	l.added = append(l.added, a.String())
}

// DeleteClientSession records old tuple retirement for ownership assertions.
func (l *migrationIdentity) DeleteClientSession(a net.Addr, owner uint32) {
	l.removed = append(l.removed, a.String())
}

// migrationServer supplies a conversation-aware backend without privileged IO.
func migrationServer(t *testing.T) *kcplib.Listener {
	t.Helper()
	p, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l, err := kcplib.ServeConversationConn(nil, 0, 0, p, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close(); p.Close() })
	return l
}

// acceptedMigrationConn creates a real accepted KCP/mux carrier from a chosen
// loopback IP, making peer scope checks use actual socket addresses.
func acceptedMigrationConn(t *testing.T, l *kcplib.Listener, ip string) *kcp.Conn {
	server, _ := acceptedMigrationPair(t, l, ip)
	return server
}

// acceptedMigrationPair exposes both mux ends for lost-control-reply tests.
func acceptedMigrationPair(t *testing.T, l *kcplib.Listener, ip string) (*kcp.Conn, *kcp.Conn) {
	t.Helper()
	p, err := net.ListenPacket("udp", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := kcplib.ServeConversationConn(nil, 0, 0, p, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close(); p.Close() })
	c, err := d.DialConversation(l.Addr())
	if err != nil {
		t.Fatal(err)
	}
	c.SetNoDelay(1, 10, 2, 1)
	c.SetWriteDelay(false)
	l.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err = c.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	s, err := l.AcceptKCP()
	if err != nil {
		t.Fatal(err)
	}
	s.SetDeadline(time.Now().Add(5 * time.Second))
	var b [1]byte
	if _, err = io.ReadFull(s, b[:]); err != nil {
		t.Fatal(err)
	}
	s.SetDeadline(time.Time{})
	mux, err := smux.Server(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn := &kcp.Conn{UDPSession: s, Session: mux}
	t.Cleanup(func() { conn.Close(); c.Close() })
	clientMux, err := smux.Client(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &kcp.Conn{UDPSession: c, Session: clientMux}
	t.Cleanup(func() { client.Close() })
	return conn, client
}

// TestMigrationCapabilityScopeAndReplay verifies check-before-move, idempotent
// commit, bounded token ownership and rejection of stale/wrong-listener moves.
func TestMigrationCapabilityScopeAndReplay(t *testing.T) {
	e := reloadFixture(t)
	l := migrationServer(t)
	identity := &migrationIdentity{}
	old := acceptedMigrationConn(t, l, "127.0.0.1")
	via := acceptedMigrationConn(t, l, "127.0.0.1")
	otherIP := acceptedMigrationConn(t, l, "127.0.0.2")
	token := e.migrationControl(identity, old, protocol.Proto{Type: protocol.PMTOKEN})
	if token.Status != 0 || token.Capability == ([32]byte{}) {
		t.Fatal("capability unavailable")
	}
	again := e.migrationControl(identity, old, protocol.Proto{Type: protocol.PMTOKEN})
	if again.Capability != token.Capability || len(e.migrations.entries) != 1 {
		t.Fatal("duplicate capability state")
	}
	request := protocol.Proto{Type: protocol.PMCHECK, Capability: token.Capability, Epoch: 1}
	before := old.RemoteAddr().String()
	mux := old.Session
	conv := old.UDPSession.GetConv()
	if r := e.migrationControl(identity, via, request); r.Status != 0 {
		t.Fatal("valid check rejected")
	}
	if old.RemoteAddr().String() != before {
		t.Fatal("check moved session")
	}
	for _, invalid := range []struct {
		listener tnet.Listener
		via      *kcp.Conn
		request  protocol.Proto
	}{
		{&migrationIdentity{}, via, request}, {identity, otherIP, request},
		{identity, via, protocol.Proto{Type: protocol.PMMOVE, Capability: token.Capability, Epoch: 0}},
		{identity, via, protocol.Proto{Type: protocol.PMMOVE, Capability: [32]byte{7}, Epoch: 1}},
	} {
		if r := e.migrationControl(invalid.listener, invalid.via, invalid.request); r.Status == 0 {
			t.Fatal("invalid migration accepted")
		}
	}
	request.Type = protocol.PMMOVE
	for range 3 {
		if r := e.migrationControl(identity, via, request); r.Status != 0 {
			t.Fatal("idempotent commit rejected")
		}
	}
	if old.Session != mux || old.UDPSession.GetConv() != conv || old.RemoteAddr().String() != via.RemoteAddr().String() {
		t.Fatal("logical identity lost")
	}
	if len(identity.added) != 1 || len(identity.removed) != 1 || identity.removed[0] != before {
		t.Fatal("duplicate/incorrect encoder ownership")
	}
	next := acceptedMigrationConn(t, l, "127.0.0.1")
	request.Epoch = 2
	if r := e.migrationControl(identity, next, request); r.Status != 0 {
		t.Fatal("second move rejected")
	}
	request.Epoch = 1
	if r := e.migrationControl(identity, via, request); r.Status == 0 {
		t.Fatal("stale move reverted tuple")
	}
	old.Close()
	e.forgetMigration(old)
	if len(e.migrations.entries) != 0 {
		t.Fatal("closed capability leaked")
	}
}

// TestMigrationConfigRejectsUnsupportedOwnership keeps migration opt-in and
// rejects shared client ownership or disabled recovery before staging sockets.
func TestMigrationConfigRejectsUnsupportedOwnership(t *testing.T) {
	for _, test := range []struct{ enabled, listener, shared, valid bool }{{true, false, false, true}, {false, false, false, false}, {true, true, false, false}, {true, false, true, false}} {
		cfg := PathRecoveryConfig{Enabled: test.enabled, PreserveConnections: true}
		if err := cfg.prepare(test.listener, test.shared); (err == nil) != test.valid {
			t.Fatalf("unexpected validation: %+v %v", test, err)
		}
	}
}

// TestMigrationWarningIsBounded verifies no false warning for healthy/transient
// observations, one warning for a qualified failure, and a finite watch period.
func TestMigrationWarningIsBounded(t *testing.T) {
	start := time.Now()
	w := migrationObservation{at: start}
	if progress, warn := w.sample(start.Add(2*time.Second), 0, false); progress || warn {
		t.Fatal("brief gap warned")
	}
	if progress, warn := w.sample(start.Add(3*time.Second), 12, false); !progress || warn {
		t.Fatal("progress was not classified")
	}
	if progress, warn := w.sample(start.Add(4*time.Second), 24, false); progress || warn {
		t.Fatal("healthy logging repeated")
	}
	if _, warn := w.sample(start.Add(19*time.Second), 24, true); !warn {
		t.Fatal("early stall invisible")
	}
	if _, warn := w.sample(start.Add(20*time.Second), 24, true); warn {
		t.Fatal("stall warning repeated")
	}
	w = migrationObservation{at: start}
	if progress, warn := w.sample(start.Add(61*time.Second), 0, true); progress || warn {
		t.Fatal("watch did not expire")
	}
}

// TestMigrationLostCommitReply retries a real control stream after the first
// response disappears. The backend moves exactly once and retains its mux.
func TestMigrationLostCommitReply(t *testing.T) {
	e := reloadFixture(t)
	l := migrationServer(t)
	identity := &migrationIdentity{}
	old := acceptedMigrationConn(t, l, "127.0.0.1")
	via, control := acceptedMigrationPair(t, l, "127.0.0.1")
	token := e.migrationControl(identity, old, protocol.Proto{Type: protocol.PMTOKEN})
	request := protocol.Proto{Type: protocol.PMMOVE, Capability: token.Capability, Epoch: 1}
	var requests atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			stream, err := via.Session.AcceptStream()
			if err != nil {
				return
			}
			var incoming protocol.Proto
			if incoming.Read(stream) == nil {
				reply := e.migrationControl(identity, via, incoming)
				if requests.Add(1) > 1 {
					reply.Write(stream)
				}
			}
			stream.Close()
		}
	}()
	e.finishMigration(&peer{engine: e, ctx: e.ctx, endpoint: Endpoint{PathRecovery: recoveryConfig(t)}}, "test", old, control, request)
	via.Close()
	<-done
	if requests.Load() != 2 || len(identity.added) != 1 || old.Session.IsClosed() {
		t.Fatal("lost reply did not retain/idempotently commit live carrier")
	}
	// A skipped local generation can supersede a commit that never arrived.
	next := acceptedMigrationConn(t, l, "127.0.0.1")
	request.Epoch = 3
	if r := e.migrationControl(identity, next, request); r.Status != 0 {
		t.Fatal("newer recovery cannot supersede ambiguous commit")
	}
	request.Epoch = 2
	if r := e.migrationControl(identity, via, request); r.Status == 0 {
		t.Fatal("delayed lower generation accepted")
	}
}
