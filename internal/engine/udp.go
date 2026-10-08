//go:build linux

// File udp.go: maps local UDP sources to reliable mux streams with bounded queues and explicit
// datagram framing.

package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"sync"
	"sync/atomic"
	"time"
)

// packet retains a pooled datagram buffer, its size class and live byte length until send or
// expiry recycles it.
type packet struct {
	// Pooled payload slice whose handle must be returned after consumption or drop.
	b *[]byte
	// Pool size class and live payload bytes, retained for exact recycling and framing.
	class, n int
}

// udpFlow retains one local UDP source conversation, its bounded pending queue and idempotent
// expiry signal.
type udpFlow struct {
	// At most eight pending local datagrams; discarded pre-admission bytes cannot be recovered
	// by KCP.
	queue chan packet
	// Broadcasts flow shutdown to sender/receiver/expiry paths.
	done chan struct{}
	// Makes concurrent lifecycle completion idempotent.
	once sync.Once
	// Atomic activity time used by the shared idle-expiry sweep.
	last atomic.Int64
}

// close signals flow cancellation exactly once so expiry, receive failure and engine shutdown
// can race safely.
func (f *udpFlow) close() { f.once.Do(func() { close(f.done) }) }

// writeDatagram writes a bounded length-prefixed record, including empty UDP payloads, over
// the reliable stream.
func writeDatagram(w io.Writer, b []byte) error {
	if len(b) > 65507 {
		return errors.New("UDP datagram too large")
	}
	if len(b) <= 2048 {
		// One small record avoids two mux writes. A pooled buffer also avoids
		// a 2 KiB heap escape on every interface Write; no flow retains it.
		buffer, class := getBuffer(2 + len(b))
		defer copyPools[class].Put(buffer)
		buf := (*buffer)[:2+len(b)]
		binary.BigEndian.PutUint16(buf[:2], uint16(len(b)))
		copy(buf[2:], b)
		n, err := w.Write(buf)
		if err != nil {
			return err
		} else if n != 2+len(b) {
			return io.ErrShortWrite
		}
		return nil
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(b)))
	if n, err := w.Write(hdr[:]); err != nil {
		return err
	} else if n != 2 {
		return io.ErrShortWrite
	}
	if n, err := w.Write(b); err != nil {
		return err
	} else if n != len(b) {
		return io.ErrShortWrite
	}
	return nil
}

// readDatagram reads exactly one bounded record into pooled storage and recycles the
// allocation on truncated input.
func readDatagram(r io.Reader) (packet, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return packet{}, err
	}
	n := int(binary.BigEndian.Uint16(hdr[:]))
	if n > 65507 {
		return packet{}, errors.New("invalid UDP datagram size")
	}
	b, class := getBuffer(n)
	if _, err := io.ReadFull(r, (*b)[:n]); err != nil {
		copyPools[class].Put(b)
		return packet{}, err
	}
	return packet{b, class, n}, nil
}

// relayUDP bridges framed stream records and a connected destination UDP socket with
// independent inactivity deadlines.
func (e *Engine) relayUDP(conn *net.UDPConn, strm tnet.Strm) {
	defer conn.Close()
	defer strm.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer strm.Close()
		defer conn.Close()
		buf := make([]byte, 65508)
		for {
			conn.SetReadDeadline(time.Now().Add(e.current().Limits.UDPDuration))
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if n > 65507 {
				e.stats.Errors.Add(1)
				continue
			}
			if err := writeDatagram(strm, buf[:n]); err != nil {
				return
			}
			e.stats.Sent.Add(int64(n))
		}
	}()
	for {
		strm.SetReadDeadline(time.Now().Add(e.current().Limits.UDPDuration))
		p, err := readDatagram(strm)
		if err != nil {
			break
		}
		_, err = conn.Write((*p.b)[:p.n])
		copyPools[p.class].Put(p.b)
		if err != nil {
			break
		}
		e.stats.Received.Add(int64(p.n))
	}
	conn.Close()
	strm.Close()
	<-done
}

// serveUDP owns one staged local UDP bind's source-flow map, bounded queues and
// expiry. New flows capture current routing; existing sources retain their target.
func (e *Engine) serveUDP(ctx context.Context, conn *net.UDPConn, key string) {
	flows := map[netip.AddrPort]*udpFlow{}
	var mu sync.Mutex
	e.launch(func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				mu.Lock()
				for _, v := range flows {
					v.close()
				}
				mu.Unlock()
				return
			case now := <-ticker.C:
				mu.Lock()
				for addr, v := range flows {
					if now.UnixNano()-v.last.Load() > int64(e.current().Limits.UDPDuration) {
						delete(flows, addr)
						v.close()
					}
				}
				mu.Unlock()
			}
		}
	})
	e.launch(func() {
		buf := make([]byte, 65508)
		for {
			n, addr, err := conn.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			if n > 65507 {
				e.stats.Errors.Add(1)
				continue
			}
			mu.Lock()
			flow := flows[addr]
			if flow == nil {
				binding, ok := e.route(key)
				if !ok {
					mu.Unlock()
					continue
				}
				f, peer := binding.forward, binding.peer
				if !e.acquire() {
					mu.Unlock()
					continue
				}
				flow = &udpFlow{queue: make(chan packet, 8), done: make(chan struct{})}
				flow.last.Store(time.Now().UnixNano())
				flows[addr] = flow
				v := flow
				e.launch(func() {
					defer e.stats.Active.Add(-1)
					defer func() {
						v.close()
						mu.Lock()
						if flows[addr] == v {
							delete(flows, addr)
						}
						mu.Unlock()
						for {
							select {
							case p := <-v.queue:
								copyPools[p.class].Put(p.b)
							default:
								return
							}
						}
					}()
					openCtx, cancel := context.WithTimeout(ctx, e.current().Limits.OpenDuration)
					strm, err := peer.open(openCtx, protocol.PUDP2, f.Target)
					cancel()
					if err != nil {
						e.stats.Errors.Add(1)
						return
					}
					defer strm.Close()
					stop := context.AfterFunc(ctx, func() { strm.Close() })
					defer stop()
					readDone := make(chan struct{})
					go func() {
						defer close(readDone)
						defer v.close()
						for {
							strm.SetReadDeadline(time.Now().Add(e.current().Limits.UDPDuration))
							p, err := readDatagram(strm)
							if err != nil {
								return
							}
							_, err = conn.WriteToUDPAddrPort((*p.b)[:p.n], addr)
							copyPools[p.class].Put(p.b)
							if err != nil {
								return
							}
							v.last.Store(time.Now().UnixNano())
							e.stats.Received.Add(int64(p.n))
						}
					}()
					defer func() { strm.Close(); <-readDone }()
					for {
						select {
						case <-v.done:
							return
						case <-ctx.Done():
							return
						case p := <-v.queue:
							err := writeDatagram(strm, (*p.b)[:p.n])
							copyPools[p.class].Put(p.b)
							if err != nil {
								return
							}
							e.stats.Sent.Add(int64(p.n))
						}
					}
				})
			}
			flow.last.Store(time.Now().UnixNano())
			// Enqueue under the map lock, so removal/draining cannot race with a sender.
			select {
			case <-flow.done:
			default:
				if len(flow.queue) < cap(flow.queue) {
					b, class := getBuffer(n)
					copy(*b, buf[:n])
					select {
					case flow.queue <- packet{b, class, n}:
					default:
						copyPools[class].Put(b)
						e.stats.Rejected.Add(1)
					}
				} else {
					e.stats.Rejected.Add(1)
				}
			}
			mu.Unlock()
		}
	})
}
