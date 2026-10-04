//go:build linux

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

type packet struct {
	b        *[]byte
	class, n int
}
type udpFlow struct {
	queue chan packet
	done  chan struct{}
	once  sync.Once
	last  atomic.Int64
}

func (f *udpFlow) close() { f.once.Do(func() { close(f.done) }) }

func writeDatagram(w io.Writer, b []byte) error {
	if len(b) > 65507 {
		return errors.New("UDP datagram too large")
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(b)))
	if n, err := w.Write(hdr[:]); err != nil {
		return err
	} else if n != 2 {
		return io.ErrShortWrite
	}
	if len(b) == 0 {
		return nil
	}
	if n, err := w.Write(b); err != nil {
		return err
	} else if n != len(b) {
		return io.ErrShortWrite
	}
	return nil
}

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
			conn.SetReadDeadline(time.Now().Add(e.cfg.Limits.UDPDuration))
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
		strm.SetReadDeadline(time.Now().Add(e.cfg.Limits.UDPDuration))
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

func (e *Engine) startUDP(f Forward) error {
	a, err := net.ResolveUDPAddr("udp", f.Listen)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", a)
	if err != nil {
		return err
	}
	e.closers = append(e.closers, conn)
	flows := map[netip.AddrPort]*udpFlow{}
	var mu sync.Mutex
	e.launch(func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-e.ctx.Done():
				mu.Lock()
				for _, v := range flows {
					v.close()
				}
				mu.Unlock()
				return
			case now := <-ticker.C:
				mu.Lock()
				for addr, v := range flows {
					if now.UnixNano()-v.last.Load() > int64(e.cfg.Limits.UDPDuration) {
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
					ctx, cancel := context.WithTimeout(e.ctx, e.cfg.Limits.OpenDuration)
					strm, err := e.peers[f.Peer].open(ctx, protocol.PUDP2, f.Target)
					cancel()
					if err != nil {
						e.stats.Errors.Add(1)
						return
					}
					defer strm.Close()
					stop := context.AfterFunc(e.ctx, func() { strm.Close() })
					defer stop()
					readDone := make(chan struct{})
					go func() {
						defer close(readDone)
						defer v.close()
						for {
							strm.SetReadDeadline(time.Now().Add(e.cfg.Limits.UDPDuration))
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
						case <-e.ctx.Done():
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
	return nil
}
