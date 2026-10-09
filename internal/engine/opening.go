//go:build linux

// File opening.go owns one target dial across retries of an unfinished opening.
// A slow mux consumer can hide even a successfully sent receipt. Request IDs
// therefore outlive individual carrier streams; only a client commit transfers
// the target socket to a relay. Established forwards never enter this registry.
package engine

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"paqet/internal/protocol"
	"paqet/internal/tnet"
)

// openingKey scopes identities to a listener generation, including its dial policy.
// Retries use the same listener but may arrive through independent source ports.
type openingKey struct {
	listener tnet.Listener
	id       [16]byte
}

// openingTicket retains pending ownership or a small replay tombstone until expiry.
// Fields other than immutable key/target/deadline are protected by registry.mu.
type openingTicket struct {
	key             openingKey
	target          string
	kind            byte
	deadline        time.Time
	ready           chan struct{}
	conn            net.Conn
	err             error
	finished, taken bool
	pending         bool
}

// openingHeap schedules receipt expiry with one engine timer, not one idle timer
// or goroutine per successful customer connection.
type openingHeap []*openingTicket

// Len reports retained expiry records.
func (h openingHeap) Len() int { return len(h) }

// Less keeps the nearest deadline at the root.
func (h openingHeap) Less(i, j int) bool { return h[i].deadline.Before(h[j].deadline) }

// Swap updates heap placement without changing ticket identity.
func (h openingHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

// Push accepts only registry-owned tickets through container/heap.
func (h *openingHeap) Push(v any) { *h = append(*h, v.(*openingTicket)) }

// Pop clears the vacated reference so expiry releases its storage.
func (h *openingHeap) Pop() any {
	old := *h
	n := len(old) - 1
	v := old[n]
	old[n] = nil
	*h = old[:n]
	return v
}

// openingRegistry serializes publication/claim only; target dialing, protocol
// I/O and socket closure always occur outside its lock.
type openingRegistry struct {
	mu      sync.Mutex
	entries map[openingKey]*openingTicket
	expiry  openingHeap
	closed  bool
	pending int
}

// reserve returns the existing dial on replay and rejects conflicting requests.
// Capacity is derived from the configured admission ceiling and includes replay
// tombstones, bounding retained memory even under rapid connection churn.
func (r *openingRegistry) reserve(ctx context.Context, listener tnet.Listener, p protocol.Proto, limit int64, deadline time.Time) (*openingTicket, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Cancellation is checked while holding the same lock as listener retirement:
	// a stopped generation cannot publish new pending targets after cleanup.
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if r.closed {
		return nil, false, net.ErrClosed
	}
	if r.entries == nil {
		r.entries = make(map[openingKey]*openingTicket)
	}
	key := openingKey{listener, p.RequestID}
	if t := r.entries[key]; t != nil {
		if t.kind != p.Type || t.target != p.Addr.String() {
			return nil, false, errors.New("opening identity reused with different target")
		}
		if t.taken || !time.Now().Before(t.deadline) {
			return nil, false, errors.New("opening already committed or expired")
		}
		return t, false, nil
	}
	if int64(len(r.entries)) >= limit {
		return nil, false, errors.New("opening receipt capacity exhausted")
	}
	t := &openingTicket{key: key, target: p.Addr.String(), kind: p.Type, deadline: deadline, pending: true}
	r.entries[key] = t
	r.pending++
	heap.Push(&r.expiry, t)
	return t, true, nil
}

// finish publishes the single dial result; a concurrent shutdown/expiry still
// owns rejection and must close a socket that finished after its ticket retired.
func (r *openingRegistry) finish(t *openingTicket, conn net.Conn, err error) {
	r.mu.Lock()
	valid := r.entries[t.key] == t && !r.closed && !t.finished
	if valid {
		t.conn, t.err, t.finished = conn, err, true
		if err != nil && t.pending {
			t.pending = false
			r.pending--
		}
		if t.ready != nil {
			close(t.ready)
			t.ready = nil
		}
	}
	r.mu.Unlock()
	if !valid && conn != nil {
		conn.Close()
	}
}

// waitReady allocates a shared wakeup only when a replay actually joins an
// unfinished dial. Ordinary successful openings retain no channel allocation in
// their tombstone. Waiters copy the channel under the mutex; finish/retirement
// may clear the ticket's reference without racing or losing their wakeup.
func (r *openingRegistry) waitReady(t *openingTicket) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[t.key] != t || t.finished {
		return nil
	}
	if t.ready == nil {
		t.ready = make(chan struct{})
	}
	return t.ready
}

// result verifies pending ownership without exposing the socket to a handler
// before its commit. Failed dials remain replayable errors, never repeated dials.
func (r *openingRegistry) result(t *openingTicket) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[t.key] != t || t.taken || !time.Now().Before(t.deadline) {
		return net.ErrClosed
	}
	return t.err
}

// claim transfers the target exactly once after the winning stream's commit.
// Retain the identity through its original opening budget so a delayed request
// on a discarded carrier cannot establish a second target after this transfer.
func (r *openingRegistry) claim(t *openingTicket) (net.Conn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[t.key] != t || !t.finished || t.taken || t.err != nil || t.conn == nil || !time.Now().Before(t.deadline) {
		return nil, net.ErrClosed
	}
	c := t.conn
	t.conn = nil
	t.taken = true
	if t.pending {
		t.pending = false
		r.pending--
	}
	return c, nil
}

// retire releases all expired pending sockets, retaining no completed target.
// closeAll is used at engine shutdown; the sweeper otherwise touches only the
// earliest deadline, avoiding a scan of all receipts on each controller tick.
func (r *openingRegistry) retire(now time.Time, closeAll bool) {
	for {
		var sockets []net.Conn
		r.mu.Lock()
		if closeAll {
			r.closed = true
		}
		// Limit each critical section during a churn burst. The whole expiry
		// pass still finishes, but new dials/claims can interleave between groups.
		for n := 0; n < 128 && len(r.expiry) > 0 && (closeAll || !now.Before(r.expiry[0].deadline)); n++ {
			t := heap.Pop(&r.expiry).(*openingTicket)
			delete(r.entries, t.key)
			if t.pending {
				t.pending = false
				r.pending--
			}
			if t.conn != nil {
				sockets = append(sockets, t.conn)
				t.conn = nil
			}
			if !t.finished {
				t.finished = true
				t.err = context.DeadlineExceeded
				if t.ready != nil {
					close(t.ready)
					t.ready = nil
				}
			}
		}
		more := len(r.expiry) > 0 && (closeAll || !now.Before(r.expiry[0].deadline))
		if len(r.entries) == 0 {
			r.entries = nil
			r.expiry = nil
		}
		r.mu.Unlock()
		for _, c := range sockets {
			c.Close()
		}
		if !more {
			return
		}
		if !closeAll {
			runtime.Gosched() // Expiry bursts must share CPU with live opening work.
		}
	}
}

// close releases unfinished target ownership; relays own committed targets.
func (r *openingRegistry) close() { r.retire(time.Time{}, true) }

// retireListener releases a stopped listener generation immediately. Reload is
// rare, so rebuilding the expiry heap here avoids extra per-opening indexes or
// locks on the hot path. Committed relays keep ownership of their target sockets.
// Call only after cancelling the generation context used by reserve.
func (r *openingRegistry) retireListener(listener tnet.Listener) {
	var sockets []net.Conn
	r.mu.Lock()
	kept := r.expiry[:0]
	for _, t := range r.expiry {
		if t.key.listener != listener {
			kept = append(kept, t)
			continue
		}
		delete(r.entries, t.key)
		if t.pending {
			t.pending = false
			r.pending--
		}
		if t.conn != nil {
			sockets = append(sockets, t.conn)
			t.conn = nil
		}
		if !t.finished {
			t.finished, t.err = true, context.Canceled
			if t.ready != nil {
				close(t.ready)
				t.ready = nil
			}
		}
	}
	clear(r.expiry[len(kept):])
	r.expiry = kept
	heap.Init(&r.expiry)
	if len(r.entries) == 0 {
		r.entries, r.expiry = nil, nil
	}
	r.mu.Unlock()
	for _, c := range sockets {
		c.Close()
	}
}

// sweepOpenings uses one coarse engine timer. Per-ticket checks also enforce
// the exact deadline, so sweeper granularity cannot admit an expired commit.
func (e *Engine) sweepOpenings() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			e.openings.retire(now, false)
		case <-e.ctx.Done():
			return
		}
	}
}

// handleOpening confirms receipt, reuses one target dial and waits for a commit
// before starting any target reads. This preserves banners and early data when
// the client discards a full carrier and retries on a healthy sibling.
func (e *Engine) handleOpening(ctx context.Context, listener tnet.Listener, strm tnet.Strm, p protocol.Proto, trace uint64) {
	if err := writeOpeningAck(strm, 2); err != nil {
		return
	}
	t, fresh, err := e.openings.reserve(ctx, listener, p, 2*e.current().Limits.Connections, time.Now().Add(e.current().Limits.OpenDuration))
	if err != nil {
		writeOpeningAck(strm, 1)
		e.log().Debug("opening.identity_rejected", "error", err)
		return
	}
	if !fresh {
		e.stats.OpenReused.Add(1)
	}
	if fresh {
		dialCtx, cancel := context.WithDeadline(ctx, t.deadline)
		dialCtx, dialCancel := context.WithTimeout(dialCtx, e.current().Limits.DialDuration)
		proto := "tcp"
		if p.Type == protocol.PUDP3 {
			proto = "udp"
		}
		var c net.Conn
		var err error
		if e.openingDial != nil {
			c, err = e.openingDial(dialCtx, proto, t.target)
		} else {
			c, err = (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext(dialCtx, proto, t.target)
		}
		dialCancel()
		cancel()
		e.openings.finish(t, c, err)
	}
	if !fresh {
		if ready := e.openings.waitReady(t); ready != nil {
			select {
			case <-ready:
			case <-ctx.Done():
				return
			}
		}
	}
	if err := e.openings.result(t); err != nil {
		writeOpeningAck(strm, 1)
		if fresh {
			e.report(fmt.Errorf("dial %s: %w", t.target, err))
		}
		return
	}
	if err := writeOpeningAck(strm, 0); err != nil {
		return
	}
	var commit [1]byte
	if _, err := io.ReadFull(strm, commit[:]); err != nil || commit[0] != 0 {
		return
	}
	c, err := e.openings.claim(t)
	if err != nil {
		return
	}
	defer c.Close()
	strm.SetDeadline(time.Time{})
	if p.Type == protocol.PTCP3 {
		e.relayContext(ctx, c.(*net.TCPConn), strm, trace)
	} else {
		e.relayUDP(c.(*net.UDPConn), strm)
	}
}

// counts publishes bounded registry occupancy without scanning retained IDs.
func (r *openingRegistry) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries), r.pending
}

// writeOpeningCommit folds already queued TCP application bytes into the winning
// commit frame. This avoids a separate tiny KCP packet on client-first traffic.
// One nonblocking read never delays server-first protocols; an empty socket sends
// the ordinary immediate commit. Scratch is returned after mux write ownership
// transfers, and only accepted application bytes enter the relay byte counter.
// Any read/write failure is terminal: a commit may already have reached the peer.
func writeOpeningCommit(stream tnet.Strm, tcp *net.TCPConn, sent *atomic.Int64) error {
	if tcp == nil {
		return writeOpeningAck(stream, 0)
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return err
	}
	b, class := getBuffer(4096)
	defer func() {
		if b != nil {
			copyPools[class].Put(b)
		}
	}()
	var n int
	var readErr error
	err = raw.Read(func(fd uintptr) bool {
		// smux priority writes admit at most 512 bytes, including the commit.
		// Leave larger first requests queued for ordinary ordered TCP relay.
		n, readErr = unix.Read(int(fd), (*b)[1:512])
		if errors.Is(readErr, unix.EAGAIN) || errors.Is(readErr, unix.EINTR) {
			n, readErr = 0, nil
		}
		return true // Do not enter netpoll waiting for application bytes.
	})
	if err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	if n == 0 {
		copyPools[class].Put(b)
		b = nil // Empty/server-first openings retain no scratch while committing.
		return writeOpeningAck(stream, 0)
	}
	(*b)[0] = 0
	var written int
	if priority, ok := stream.(interface{ WritePriority([]byte) (int, error) }); ok {
		written, err = priority.WritePriority((*b)[:n+1])
	} else {
		written, err = stream.Write((*b)[:n+1])
	}
	sent.Add(int64(min(n, max(0, written-1))))
	if err == nil && written != n+1 {
		return io.ErrShortWrite
	}
	return err
}
