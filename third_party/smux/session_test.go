// MIT License
//
// Copyright (c) 2016-2017 xtaci
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// File session_test.go: exercises session regressions; fixtures must preserve cleanup and
// expose byte/lifecycle failures explicitly.

package smux

import (
	"bytes"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupServer starts new server listening on a random localhost port and
// returns address of the server, function to stop the server, new client
// connection to this server or an error.
func setupServer(tb testing.TB) (addr string, stopfunc func(), client net.Conn, err error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", nil, nil, err
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleConnection(conn)
	}()
	addr = ln.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		ln.Close()
		return "", nil, nil, err
	}
	return ln.Addr().String(), func() { ln.Close() }, conn, nil
}

// handleConnection runs the legacy mux echo fixture and releases accepted stream resources
// afterward.
func handleConnection(conn net.Conn) {
	session, _ := Server(conn, nil)
	for {
		if stream, err := session.AcceptStream(); err == nil {
			go func(s io.ReadWriteCloser) {
				buf := make([]byte, 65536)
				for {
					n, err := s.Read(buf)
					if err != nil {
						return
					}
					s.Write(buf[:n])
				}
			}(stream)
		} else {
			return
		}
	}
}

// setupServer starts new server listening on a random localhost port and
// returns address of the server, function to stop the server, new client
// connection to this server or an error.
func setupServerV2(tb testing.TB) (addr string, stopfunc func(), client net.Conn, err error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", nil, nil, err
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleConnectionV2(conn)
	}()
	addr = ln.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		ln.Close()
		return "", nil, nil, err
	}
	return ln.Addr().String(), func() { ln.Close() }, conn, nil
}

// handleConnectionV2 runs the credit-controlled mux echo fixture for version-two flow-control
// checks.
func handleConnectionV2(conn net.Conn) {
	config := DefaultConfig()
	config.Version = 2
	session, _ := Server(conn, config)
	for {
		if stream, err := session.AcceptStream(); err == nil {
			go func(s io.ReadWriteCloser) {
				buf := make([]byte, 65536)
				for {
					n, err := s.Read(buf)
					if err != nil {
						return
					}
					s.Write(buf[:n])
				}
			}(stream)
		} else {
			return
		}
	}
}

// TestEcho checks Echo so a change cannot silently weaken the recorded regression contract.
func TestEcho(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	const N = 100
	buf := make([]byte, 10)
	var sent string
	var received string
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		stream.Write([]byte(msg))
		sent += msg
		if n, err := stream.Read(buf); err != nil {
			t.Fatal(err)
		} else {
			received += string(buf[:n])
		}
	}
	if sent != received {
		t.Fatal("data mimatch")
	}
	session.Close()
}

// TestWriteTo checks Write To so a change cannot silently weaken the recorded regression
// contract.
func TestWriteTo(t *testing.T) {
	const N = 1 << 20
	// server
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		session, _ := Server(conn, nil)
		for {
			if stream, err := session.AcceptStream(); err == nil {
				go func(s io.ReadWriteCloser) {
					numBytes := 0
					buf := make([]byte, 65536)
					for {
						n, err := s.Read(buf)
						if err != nil {
							return
						}
						s.Write(buf[:n])
						numBytes += n

						if numBytes == N {
							s.Close()
							return
						}
					}
				}(stream)
			} else {
				return
			}
		}
	}()

	addr := ln.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// client
	session, _ := Client(conn, nil)
	stream, _ := session.OpenStream()
	sndbuf := make([]byte, N)
	for i := range sndbuf {
		sndbuf[i] = byte(rand.Int())
	}

	go stream.Write(sndbuf)

	var rcvbuf bytes.Buffer
	nw, ew := stream.WriteTo(&rcvbuf)
	if ew != io.EOF {
		t.Fatal(ew)
	}

	if nw != N {
		t.Fatal("WriteTo nw mismatch", nw)
	}

	if !bytes.Equal(sndbuf, rcvbuf.Bytes()) {
		t.Fatal("mismatched echo bytes")
	}
	t.Log(stream)
}

// TestWriteToV2 checks Write To V2 so a change cannot silently weaken the recorded regression
// contract.
func TestWriteToV2(t *testing.T) {
	config := DefaultConfig()
	config.Version = 2
	const N = 1 << 20
	// server
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		session, _ := Server(conn, config)
		for {
			if stream, err := session.AcceptStream(); err == nil {
				go func(s io.ReadWriteCloser) {
					numBytes := 0
					buf := make([]byte, 65536)
					for {
						n, err := s.Read(buf)
						if err != nil {
							return
						}
						s.Write(buf[:n])
						numBytes += n

						if numBytes == N {
							s.Close()
							return
						}
					}
				}(stream)
			} else {
				return
			}
		}
	}()

	addr := ln.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// client
	session, _ := Client(conn, config)
	stream, _ := session.OpenStream()
	sndbuf := make([]byte, N)
	for i := range sndbuf {
		sndbuf[i] = byte(rand.Int())
	}

	go stream.Write(sndbuf)

	var rcvbuf bytes.Buffer
	nw, ew := stream.WriteTo(&rcvbuf)
	if ew != io.EOF {
		t.Fatal(ew)
	}

	if nw != N {
		t.Fatal("WriteTo nw mismatch", nw, N)
	}

	if !bytes.Equal(sndbuf, rcvbuf.Bytes()) {
		t.Fatal("mismatched echo bytes")
	}

	t.Log(stream)
}

// TestGetDieCh checks Get Die Ch so a change cannot silently weaken the recorded regression
// contract.
func TestGetDieCh(t *testing.T) {
	cs, ss, err := getSmuxStreamPair()
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	dieCh := ss.GetDieCh()
	errCh := make(chan error, 1)

	go func() { // server reader
		// keep reading until error
		buf := make([]byte, 1024)
		for {
			_, err := ss.Read(buf)
			if err != nil {
				ss.Close()
				return
			}
		}
	}()

	go func() {
		select {
		case <-dieCh:
			errCh <- nil
		case <-time.Tick(time.Second):
			errCh <- fmt.Errorf("wait die chan timeout")
		}
	}()
	cs.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

// TestSpeed checks Speed so a change cannot silently weaken the recorded regression contract.
func TestSpeed(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	t.Log(stream.LocalAddr(), stream.RemoteAddr())

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		buf := make([]byte, 1024*1024)
		nrecv := 0
		for {
			n, err := stream.Read(buf)
			if err != nil {
				t.Error(err)
				break
			} else {
				nrecv += n
				if nrecv == 4096*4096 {
					break
				}
			}
		}
		stream.Close()
		t.Log("time for 16MB rtt", time.Since(start))
		wg.Done()
	}()
	msg := make([]byte, 8192)
	for i := 0; i < 2048; i++ {
		stream.Write(msg)
	}
	wg.Wait()
	session.Close()
}

// TestParallel checks Parallel so a change cannot silently weaken the recorded regression
// contract.
func TestParallel(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)

	par := 1000
	messages := 100
	var wg sync.WaitGroup
	wg.Add(par)
	for i := 0; i < par; i++ {
		stream, _ := session.OpenStream()
		go func(s *Stream) {
			buf := make([]byte, 20)
			for j := 0; j < messages; j++ {
				msg := fmt.Sprintf("hello%v", j)
				s.Write([]byte(msg))
				if _, err := s.Read(buf); err != nil {
					break
				}
			}
			s.Close()
			wg.Done()
		}(stream)
	}
	t.Log("created", session.NumStreams(), "streams")
	wg.Wait()
	session.Close()
}

// TestParallelV2 checks Parallel V2 so a change cannot silently weaken the recorded regression
// contract.
func TestParallelV2(t *testing.T) {
	config := DefaultConfig()
	config.Version = 2
	_, stop, cli, err := setupServerV2(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, config)

	par := 1000
	messages := 100
	var wg sync.WaitGroup
	wg.Add(par)
	for i := 0; i < par; i++ {
		stream, _ := session.OpenStream()
		go func(s *Stream) {
			buf := make([]byte, 20)
			for j := 0; j < messages; j++ {
				msg := fmt.Sprintf("hello%v", j)
				s.Write([]byte(msg))
				if _, err := s.Read(buf); err != nil {
					break
				}
			}
			s.Close()
			wg.Done()
		}(stream)
	}
	t.Log("created", session.NumStreams(), "streams")
	wg.Wait()
	session.Close()
}

// TestCloseThenOpen checks Close Then Open so a change cannot silently weaken the recorded
// regression contract.
func TestCloseThenOpen(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	session.Close()
	if _, err := session.OpenStream(); err == nil {
		t.Fatal("opened after close")
	}
}

// TestSessionDoubleClose checks Session Double Close so a change cannot silently weaken the
// recorded regression contract.
func TestSessionDoubleClose(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	session.Close()
	if err := session.Close(); err == nil {
		t.Fatal("session double close doesn't return error")
	}
}

// TestStreamDoubleClose checks Stream Double Close so a change cannot silently weaken the
// recorded regression contract.
func TestStreamDoubleClose(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	stream.Close()
	if err := stream.Close(); err == nil {
		t.Fatal("stream double close doesn't return error")
	}
	session.Close()
}

// TestConcurrentClose checks Concurrent Close so a change cannot silently weaken the recorded
// regression contract.
func TestConcurrentClose(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	numStreams := 100
	streams := make([]*Stream, 0, numStreams)
	var wg sync.WaitGroup
	wg.Add(numStreams)
	for i := 0; i < 100; i++ {
		stream, _ := session.OpenStream()
		streams = append(streams, stream)
	}
	for _, s := range streams {
		stream := s
		go func() {
			stream.Close()
			wg.Done()
		}()
	}
	session.Close()
	wg.Wait()
}

// TestTinyReadBuffer checks Tiny Read Buffer so a change cannot silently weaken the recorded
// regression contract.
func TestTinyReadBuffer(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	const N = 100
	tinybuf := make([]byte, 6)
	var sent string
	var received string
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		sent += msg
		nsent, err := stream.Write([]byte(msg))
		if err != nil {
			t.Fatal("cannot write")
		}
		nrecv := 0
		for nrecv < nsent {
			if n, err := stream.Read(tinybuf); err == nil {
				nrecv += n
				received += string(tinybuf[:n])
			} else {
				t.Fatal("cannot read with tiny buffer")
			}
		}
	}

	if sent != received {
		t.Fatal("data mimatch")
	}
	session.Close()
}

// TestIsClose checks Is Close so a change cannot silently weaken the recorded regression
// contract.
func TestIsClose(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	session.Close()
	if !session.IsClosed() {
		t.Fatal("still open after close")
	}
}

// TestKeepAliveTimeout checks Keep Alive Timeout so a change cannot silently weaken the
// recorded regression contract.
func TestKeepAliveTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		ln.Accept()
	}()

	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	config := DefaultConfig()
	config.KeepAliveInterval = time.Second
	config.KeepAliveTimeout = 2 * time.Second
	session, _ := Client(cli, config)
	time.Sleep(3 * time.Second)
	if !session.IsClosed() {
		t.Fatal("keepalive-timeout failed")
	}
}

// blockWriteConn retains the block Write Conn fixture state used to expose failures without
// production network side effects.
type blockWriteConn struct {
	net.Conn
}

// Write holds an output operation to expose deadline/closure behavior under genuine writer
// backpressure.
func (c *blockWriteConn) Write(b []byte) (n int, err error) {
	forever := time.Hour * 24
	time.Sleep(forever)
	return c.Conn.Write(b)
}

// TestKeepAliveBlockWriteTimeout checks Keep Alive Block Write Timeout so a change cannot
// silently weaken the recorded regression contract.
func TestKeepAliveBlockWriteTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		ln.Accept()
	}()

	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	//when writeFrame block, keepalive in old version never timeout
	blockWriteCli := &blockWriteConn{cli}

	config := DefaultConfig()
	config.KeepAliveInterval = time.Second
	config.KeepAliveTimeout = 2 * time.Second
	session, _ := Client(blockWriteCli, config)
	time.Sleep(3 * time.Second)
	if !session.IsClosed() {
		t.Fatal("keepalive-timeout failed")
	}
}

// TestServerEcho checks Server Echo so a change cannot silently weaken the recorded regression
// contract.
func TestServerEcho(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		err := func() error {
			conn, err := ln.Accept()
			if err != nil {
				return err
			}
			defer conn.Close()
			session, err := Server(conn, nil)
			if err != nil {
				return err
			}
			defer session.Close()
			buf := make([]byte, 10)
			stream, err := session.OpenStream()
			if err != nil {
				return err
			}
			defer stream.Close()
			for i := 0; i < 100; i++ {
				msg := fmt.Sprintf("hello%v", i)
				stream.Write([]byte(msg))
				n, err := stream.Read(buf)
				if err != nil {
					return err
				}
				if got := string(buf[:n]); got != msg {
					return fmt.Errorf("got: %q, want: %q", got, msg)
				}
			}
			return nil
		}()
		if err != nil {
			t.Error(err)
		}
	}()

	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	if session, err := Client(cli, nil); err == nil {
		if stream, err := session.AcceptStream(); err == nil {
			buf := make([]byte, 65536)
			for {
				n, err := stream.Read(buf)
				if err != nil {
					break
				}
				stream.Write(buf[:n])
			}
		} else {
			t.Fatal(err)
		}
	} else {
		t.Fatal(err)
	}
}

// TestSendWithoutRecv checks Send Without Recv so a change cannot silently weaken the recorded
// regression contract.
func TestSendWithoutRecv(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	const N = 100
	for i := 0; i < N; i++ {
		msg := fmt.Sprintf("hello%v", i)
		stream.Write([]byte(msg))
	}
	buf := make([]byte, 1)
	if _, err := stream.Read(buf); err != nil {
		t.Fatal(err)
	}
	stream.Close()
}

// TestWriteAfterClose checks Write After Close so a change cannot silently weaken the recorded
// regression contract.
func TestWriteAfterClose(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	stream.Close()
	if _, err := stream.Write([]byte("write after close")); err == nil {
		t.Fatal("write after close failed")
	}
}

// TestReadStreamAfterSessionClose checks Read Stream After Session Close so a change cannot
// silently weaken the recorded regression contract.
func TestReadStreamAfterSessionClose(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	session.Close()
	buf := make([]byte, 10)
	if _, err := stream.Read(buf); err != nil {
		t.Log(err)
	} else {
		t.Fatal("read stream after session close succeeded")
	}
}

// TestWriteStreamAfterConnectionClose checks Write Stream After Connection Close so a change
// cannot silently weaken the recorded regression contract.
func TestWriteStreamAfterConnectionClose(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	session.conn.Close()
	if _, err := stream.Write([]byte("write after connection close")); err == nil {
		t.Fatal("write after connection close failed")
	}
}

// TestNumStreamAfterClose checks Num Stream After Close so a change cannot silently weaken the
// recorded regression contract.
func TestNumStreamAfterClose(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	if _, err := session.OpenStream(); err == nil {
		if session.NumStreams() != 1 {
			t.Fatal("wrong number of streams after opened")
		}
		session.Close()
		if session.NumStreams() != 0 {
			t.Fatal("wrong number of streams after session closed")
		}
	} else {
		t.Fatal(err)
	}
	cli.Close()
}

// TestRandomFrame checks Random Frame so a change cannot silently weaken the recorded
// regression contract.
func TestRandomFrame(t *testing.T) {
	addr, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	// pure random
	session, _ := Client(cli, nil)
	for i := 0; i < 100; i++ {
		rnd := make([]byte, rand.Uint32()%1024)
		io.ReadFull(crand.Reader, rnd)
		session.conn.Write(rnd)
	}
	cli.Close()

	// double syn
	cli, err = net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	session, _ = Client(cli, nil)
	for i := 0; i < 100; i++ {
		f := newFrame(1, cmdSYN, 1000)
		session.writeControlFrame(f)
	}
	cli.Close()

	// random cmds
	cli, err = net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	allcmds := []byte{cmdSYN, cmdFIN, cmdPSH, cmdNOP}
	session, _ = Client(cli, nil)
	for i := 0; i < 100; i++ {
		f := newFrame(1, allcmds[rand.Int()%len(allcmds)], rand.Uint32())
		session.writeControlFrame(f)
	}
	cli.Close()

	// random cmds & sids
	cli, err = net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	session, _ = Client(cli, nil)
	for i := 0; i < 100; i++ {
		f := newFrame(1, byte(rand.Uint32()), rand.Uint32())
		session.writeControlFrame(f)
	}
	cli.Close()

	// random version
	cli, err = net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	session, _ = Client(cli, nil)
	for i := 0; i < 100; i++ {
		f := newFrame(1, byte(rand.Uint32()), rand.Uint32())
		f.ver = byte(rand.Uint32())
		session.writeControlFrame(f)
	}
	cli.Close()

	// incorrect size
	cli, err = net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	session, _ = Client(cli, nil)

	f := newFrame(1, byte(rand.Uint32()), rand.Uint32())
	rnd := make([]byte, rand.Uint32()%1024)
	io.ReadFull(crand.Reader, rnd)
	f.data = rnd

	buf := make([]byte, headerSize+len(f.data))
	buf[0] = f.ver
	buf[1] = f.cmd
	binary.LittleEndian.PutUint16(buf[2:], uint16(len(rnd)+1)) /// incorrect size
	binary.LittleEndian.PutUint32(buf[4:], f.sid)
	copy(buf[headerSize:], f.data)

	session.conn.Write(buf)
	cli.Close()

	// writeFrame after die
	cli, err = net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	session, _ = Client(cli, nil)
	//close first
	session.Close()
	for i := 0; i < 100; i++ {
		f := newFrame(1, byte(rand.Uint32()), rand.Uint32())
		session.writeControlFrame(f)
	}
}

// TestWriteFrameInternal checks Write Frame Internal so a change cannot silently weaken the
// recorded regression contract.
func TestWriteFrameInternal(t *testing.T) {
	addr, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	// pure random
	session, _ := Client(cli, nil)
	for i := 0; i < 100; i++ {
		rnd := make([]byte, rand.Uint32()%1024)
		io.ReadFull(crand.Reader, rnd)
		session.conn.Write(rnd)
	}
	cli.Close()

	// writeFrame after die
	cli, err = net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	session, _ = Client(cli, nil)
	//close first
	session.Close()
	for i := 0; i < 100; i++ {
		f := newFrame(1, byte(rand.Uint32()), rand.Uint32())

		timer := time.NewTimer(session.config.KeepAliveTimeout)
		defer timer.Stop()

		session.writeFrameInternal(f, timer.C, CLSDATA)
	}

	// random cmds
	cli, err = net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	allcmds := []byte{cmdSYN, cmdFIN, cmdPSH, cmdNOP}
	session, _ = Client(cli, nil)
	for i := 0; i < 100; i++ {
		f := newFrame(1, allcmds[rand.Int()%len(allcmds)], rand.Uint32())

		timer := time.NewTimer(session.config.KeepAliveTimeout)
		defer timer.Stop()

		session.writeFrameInternal(f, timer.C, CLSDATA)
	}
	//deadline occur
	{
		c := make(chan time.Time)
		close(c)
		f := newFrame(1, allcmds[rand.Int()%len(allcmds)], rand.Uint32())
		_, err := session.writeFrameInternal(f, c, CLSDATA)
		if !strings.Contains(err.Error(), "timeout") {
			t.Fatal("write frame with deadline failed", err)
		}
	}
	cli.Close()

	{
		cli, err = net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		config := DefaultConfig()
		config.KeepAliveInterval = time.Second
		config.KeepAliveTimeout = 2 * time.Second
		session, _ = Client(&blockWriteConn{cli}, config)
		f := newFrame(1, byte(rand.Uint32()), rand.Uint32())
		c := make(chan time.Time)
		go func() {
			//die first, deadline second, better for coverage
			time.Sleep(time.Second)
			session.Close()
			time.Sleep(time.Second)
			close(c)
		}()
		_, err = session.writeFrameInternal(f, c, CLSDATA)
		if !strings.Contains(err.Error(), "closed pipe") {
			t.Fatal("write frame with to closed conn failed", err)
		}
	}
}

// TestReadDeadline checks Read Deadline so a change cannot silently weaken the recorded
// regression contract.
func TestReadDeadline(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	const N = 100
	buf := make([]byte, 10)
	var readErr error
	for i := 0; i < N; i++ {
		stream.SetReadDeadline(time.Now().Add(-1 * time.Minute))
		if _, readErr = stream.Read(buf); readErr != nil {
			break
		}
	}
	if readErr != nil {
		if !strings.Contains(readErr.Error(), "timeout") {
			t.Fatalf("Wrong error: %v", readErr)
		}
	} else {
		t.Fatal("No error when reading with past deadline")
	}
	session.Close()
}

// TestWriteDeadline checks Write Deadline so a change cannot silently weaken the recorded
// regression contract.
func TestWriteDeadline(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	buf := make([]byte, 10)
	var writeErr error
	for {
		stream.SetWriteDeadline(time.Now().Add(-1 * time.Minute))
		if _, writeErr = stream.Write(buf); writeErr != nil {
			if !strings.Contains(writeErr.Error(), "timeout") {
				t.Fatalf("Wrong error: %v", writeErr)
			}
			break
		}
	}
	session.Close()
}

// Test8GBTransferV1 checks 8 GB Transfer V1 so a change cannot silently weaken the recorded
// regression contract.
func Test8GBTransferV1(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	const N = 8 << 30 // 8GB

	testRandomLength(t, stream, N)
	session.Close()
}

// This test validates large data transfer (8GB) over a single stream with random data
func Test8GBTransferV2(t *testing.T) {
	config := DefaultConfig()
	config.Version = 2
	_, stop, cli, err := setupServerV2(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, config)
	stream, _ := session.OpenStream()
	const N = 8 << 30 // 8GB

	testRandomLength(t, stream, N)
	session.Close()
}

// Test random length with random data transfer for 1GB
func TestRandomLengthRandomDataTransferV1(t *testing.T) {
	_, stop, cli, err := setupServer(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	stream, _ := session.OpenStream()
	const N = 1 << 30 // 1GB

	testRandomLength(t, stream, N)
	session.Close()
}

// TestRandomLengthRandomDataTransferV2 checks Random Length Random Data Transfer V2 so a
// change cannot silently weaken the recorded regression contract.
func TestRandomLengthRandomDataTransferV2(t *testing.T) {
	config := DefaultConfig()
	config.Version = 2
	_, stop, cli, err := setupServerV2(t)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, config)
	stream, _ := session.OpenStream()
	const N = 1 << 30 // 1GB

	testRandomLength(t, stream, N)
	session.Close()
}

// testRandomLength varies application write sizes to expose framing boundaries that fixed-size
// traffic can hide.
func testRandomLength(t *testing.T, stream *Stream, N int64) {
	seed := time.Now().UnixNano()
	writerSrc := rand.NewSource(seed)
	readerSrc := rand.NewSource(seed)
	writerLenRand := rand.New(rand.NewSource(seed + 1))
	readerLenRand := rand.New(rand.NewSource(seed + 2))
	const maxChunk = 1 << 20

	bytesSent := int64(0)
	bytesReceived := int64(0)

	// Writer goroutine
	go func() {
		r := rand.New(writerSrc)
		sndbuf := make([]byte, maxChunk)
		lastPrint := int64(0)
		for bytesSent < N {
			length := writerLenRand.Intn(maxChunk) + 1 // Random length between 1 and 1MB
			if bytesSent+int64(length) > N {
				length = int(N - bytesSent)
			}
			buf := sndbuf[:length]
			if _, err := r.Read(buf); err != nil {
				t.Errorf("Rand read error: %v", err)
				return
			}
			n, err := stream.Write(buf)
			if err != nil {
				t.Errorf("Write error: %v", err)
				return
			}
			bytesSent += int64(n)
			if bytesSent-lastPrint >= (1 << 28) { // Log every 256MB
				lastPrint = bytesSent
				t.Log("Sent:", bytesSent, "bytes")
			}
		}
	}()

	// Reader goroutine
	r := rand.New(readerSrc)
	rcvbuf := make([]byte, maxChunk)
	expbuf := make([]byte, maxChunk)
	lastPrint := int64(0)
	for bytesReceived < N {
		length := readerLenRand.Intn(maxChunk) + 1 // Random length between 1 and 1MB
		if bytesReceived+int64(length) > N {
			length = int(N - bytesReceived)
		}
		buf := rcvbuf[:length]
		n, err := stream.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("Read error: %v", err)
		}
		if n > 0 {
			if _, err := r.Read(expbuf[:n]); err != nil {
				t.Fatalf("Rand read error: %v", err)
			}
			if !bytes.Equal(buf[:n], expbuf[:n]) {
				for i := 0; i < n; i++ {
					if buf[i] != expbuf[i] {
						t.Fatalf("Data mismatch at byte %d: got %v, want %v", bytesReceived+int64(i), buf[i], expbuf[i])
					}
				}
			}
		}
		bytesReceived += int64(n)
		if bytesReceived-lastPrint >= (1 << 28) { // Log every 256MB
			lastPrint = bytesReceived
			t.Log("Received:", bytesReceived, "bytes")
		}
	}

}

// BenchmarkAcceptClose measures Accept Close with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkAcceptClose(b *testing.B) {
	_, stop, cli, err := setupServer(b)
	if err != nil {
		b.Fatal(err)
	}
	defer stop()
	session, _ := Client(cli, nil)
	for i := 0; i < b.N; i++ {
		if stream, err := session.OpenStream(); err == nil {
			stream.Close()
		} else {
			b.Fatal(err)
		}
	}
}

// BenchmarkConnSmux measures Conn Smux with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkConnSmux(b *testing.B) {
	cs, ss, err := getSmuxStreamPair()
	if err != nil {
		b.Fatal(err)
	}
	defer cs.Close()
	defer ss.Close()
	bench(b, cs, ss)
}

// BenchmarkConnTCP measures Conn TCP with the fixture's workload; results must be interpreted
// with its buffer and transport settings.
func BenchmarkConnTCP(b *testing.B) {
	cs, ss, err := getTCPConnectionPair()
	if err != nil {
		b.Fatal(err)
	}
	defer cs.Close()
	defer ss.Close()
	bench(b, cs, ss)
}

// getSmuxStreamPair opens matching logical stream endpoints so tests exercise the same mux
// conversation.
func getSmuxStreamPair() (*Stream, *Stream, error) {
	c1, c2, err := getTCPConnectionPair()
	if err != nil {
		return nil, nil, err
	}

	s, err := Server(c2, nil)
	if err != nil {
		return nil, nil, err
	}
	c, err := Client(c1, nil)
	if err != nil {
		return nil, nil, err
	}
	var ss *Stream
	done := make(chan error)
	go func() {
		var rerr error
		ss, rerr = s.AcceptStream()
		done <- rerr
		close(done)
	}()
	cs, err := c.OpenStream()
	if err != nil {
		return nil, nil, err
	}
	err = <-done
	if err != nil {
		return nil, nil, err
	}

	return cs, ss, nil
}

// getTCPConnectionPair constructs loopback TCP endpoints used as an independent carrier
// fixture.
func getTCPConnectionPair() (net.Conn, net.Conn, error) {
	lst, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, nil, err
	}
	defer lst.Close()

	var conn0 net.Conn
	var err0 error
	done := make(chan struct{})
	go func() {
		conn0, err0 = lst.Accept()
		close(done)
	}()

	conn1, err := net.Dial("tcp", lst.Addr().String())
	if err != nil {
		return nil, nil, err
	}

	<-done
	if err0 != nil {
		return nil, nil, err0
	}
	return conn0, conn1, nil
}

// bench runs the mux throughput fixture while separating setup from repeated transfers.
func bench(b *testing.B, rd io.Reader, wr io.Writer) {
	buf := make([]byte, 128*1024)
	buf2 := make([]byte, 128*1024)
	b.SetBytes(128 * 1024)
	b.ReportAllocs()
	b.ResetTimer()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		count := 0
		for {
			n, _ := rd.Read(buf2)
			count += n
			if count == 128*1024*b.N {
				return
			}
		}
	}()
	for i := 0; i < b.N; i++ {
		wr.Write(buf)
	}
	wg.Wait()
}

// TestFrameString checks Frame String so a change cannot silently weaken the recorded
// regression contract.
func TestFrameString(t *testing.T) {
	h := rawHeader{1, cmdSYN, 100, 0, 1, 0, 0, 0}
	expected := "Version:1 Cmd:0 StreamID:1 Length:100"
	if h.String() != expected {
		t.Fatalf("expected %s, got %s", expected, h.String())
	}
}

// TestSessionAddr checks Session Addr so a change cannot silently weaken the recorded
// regression contract.
func TestSessionAddr(t *testing.T) {
	p1, p2 := net.Pipe()
	s, _ := Server(p1, nil)
	defer s.Close()
	defer p2.Close()

	if s.LocalAddr() == nil {
		t.Fatal("LocalAddr should not be nil")
	}
	if s.RemoteAddr() == nil {
		t.Fatal("RemoteAddr should not be nil")
	}
}

// TestSessionSetDeadline checks Session Set Deadline so a change cannot silently weaken the
// recorded regression contract.
func TestSessionSetDeadline(t *testing.T) {
	p1, p2 := net.Pipe()
	s, _ := Server(p1, nil)
	defer s.Close()
	defer p2.Close()

	if err := s.SetDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
}

// TestStreamID checks Stream ID so a change cannot silently weaken the recorded regression
// contract.
func TestStreamID(t *testing.T) {
	p1, p2 := net.Pipe()
	s, _ := Server(p1, nil)
	defer s.Close()
	defer p2.Close()

	go func() {
		c, _ := Client(p2, nil)
		c.OpenStream()
	}()

	stream, err := s.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	if stream.ID() == 0 {
		t.Fatal("Stream ID should not be 0")
	}
}

// TestStreamSetDeadline checks Stream Set Deadline so a change cannot silently weaken the
// recorded regression contract.
func TestStreamSetDeadline(t *testing.T) {
	p1, p2 := net.Pipe()
	s, _ := Server(p1, nil)
	defer s.Close()
	defer p2.Close()

	go func() {
		c, _ := Client(p2, nil)
		c.OpenStream()
	}()

	stream, err := s.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.SetDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
}

// TestTimeoutError checks Timeout Error so a change cannot silently weaken the recorded
// regression contract.
func TestTimeoutError(t *testing.T) {
	var err error = &timeoutError{}
	if ne, ok := err.(net.Error); ok {
		if !ne.Temporary() {
			t.Fatal("timeoutError should be temporary")
		}
		if !ne.Timeout() {
			t.Fatal("timeoutError should be a timeout")
		}
		if ne.Error() != "timeout" {
			t.Fatal("timeoutError string should be 'timeout'")
		}
	} else {
		t.Fatal("timeoutError should implement net.Error")
	}
}

// TestSessionOpenAccept checks Session Open Accept so a change cannot silently weaken the
// recorded regression contract.
func TestSessionOpenAccept(t *testing.T) {
	p1, p2 := net.Pipe()
	s, _ := Server(p1, nil)
	c, _ := Client(p2, nil)
	defer s.Close()
	defer c.Close()
	defer p1.Close()
	defer p2.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := c.Open(); err != nil {
			t.Error(err)
		}
	}()

	if _, err := s.Accept(); err != nil {
		t.Fatal(err)
	}
	<-done
}

// TestStreamAddr checks Stream Addr so a change cannot silently weaken the recorded regression
// contract.
func TestStreamAddr(t *testing.T) {
	p1, p2 := net.Pipe()
	s, _ := Server(p1, nil)
	defer s.Close()
	defer p2.Close()

	go func() {
		c, _ := Client(p2, nil)
		c.OpenStream()
	}()

	stream, err := s.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	if stream.LocalAddr() == nil {
		t.Fatal("LocalAddr should not be nil")
	}
	if stream.RemoteAddr() == nil {
		t.Fatal("RemoteAddr should not be nil")
	}
}

// hiddenConn retains the hidden Conn fixture state used to expose failures without production
// network side effects.
type hiddenConn struct {
	conn net.Conn
}

// Read delegates test input while hiding optional optimized carrier interfaces.
func (c *hiddenConn) Read(b []byte) (n int, err error) { return c.conn.Read(b) }

// Write delegates test output while hiding optional optimized carrier interfaces.
func (c *hiddenConn) Write(b []byte) (n int, err error) { return c.conn.Write(b) }

// Close releases this object's owned resources or signals its lifecycle once; shared listener
// ownership is handled by its wrapper.
func (c *hiddenConn) Close() error { return c.conn.Close() }

// TestSessionAddrNonNetConn checks Session Addr Non Net Conn so a change cannot silently
// weaken the recorded regression contract.
func TestSessionAddrNonNetConn(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()
	hc := &hiddenConn{p1}
	s, _ := Server(hc, nil)
	defer s.Close()

	if s.LocalAddr() != nil {
		t.Fatal("LocalAddr should be nil")
	}
	if s.RemoteAddr() != nil {
		t.Fatal("RemoteAddr should be nil")
	}
}

// TestStreamAddrNonNetConn checks Stream Addr Non Net Conn so a change cannot silently weaken
// the recorded regression contract.
func TestStreamAddrNonNetConn(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()
	hc := &hiddenConn{p1}
	s, _ := Server(hc, nil)
	defer s.Close()

	go func() {
		c, _ := Client(p2, nil)
		c.OpenStream()
	}()

	stream, err := s.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	if stream.LocalAddr() != nil {
		t.Fatal("LocalAddr should be nil")
	}
	if stream.RemoteAddr() != nil {
		t.Fatal("RemoteAddr should be nil")
	}
}

// TestSessionCloseChan checks Session Close Chan so a change cannot silently weaken the
// recorded regression contract.
func TestSessionCloseChan(t *testing.T) {
	p1, p2 := net.Pipe()
	s, _ := Server(p1, nil)
	defer p1.Close()
	defer p2.Close()

	ch := s.CloseChan()
	select {
	case <-ch:
		t.Fatal("CloseChan should not be closed yet")
	default:
	}

	s.Close()
	select {
	case <-ch:
	default:
		t.Fatal("CloseChan should be closed")
	}
}
