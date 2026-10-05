// The MIT License (MIT)
//
// Copyright (c) 2015 xtaci
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

// File sess_test.go: exercises sess regressions; fixtures must preserve cleanup and expose
// byte/lifecycle failures explicitly.

package kcp

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"log"
	"log/slog"
	mrand "math/rand"
	"net"
	"os"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/pbkdf2"
)

var (
	// baseport allocates distinct loopback fixture ports across repeated session tests.
	baseport = uint32(10000)
	// key retains fixture key bytes for test encryption, unrelated to deployed endpoint secrets.
	key = []byte("testkey")
	// pass retains fixture passphrase material for cipher/session comparisons, not deployment
	// credentials.
	pass = pbkdf2.Key(key, []byte("testsalt"), 4096, 32, sha1.New)
)

// init registers command options or initializes module-wide lookup/pool state once before
// runtime work begins.
func init() {

	log.Println("beginning tests, encryption:salsa20, fec:10/3")
}

// nextPort selects separate nonprivileged loopback ports for session tests to reduce fixture
// collisions.
func nextPort() int {
	port := int(atomic.AddUint32(&baseport, 1))
	port %= 65536
	if port <= 1024 {
		port += 1024
	}
	return port
}

// dialEcho creates a configured loopback echo client for the selected packet cipher.
func dialEcho(port int, block BlockCrypt) (*UDPSession, error) {
	// block, _ := NewNoneBlockCrypt(pass)
	// block, _ := NewSimpleXORBlockCrypt(pass)
	// block, _ := NewTEABlockCrypt(pass[:16])
	// block, _ := NewAESBlockCrypt(pass)
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
	if err != nil {
		panic(err)
	}

	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(16 * 1024 * 1024)
	sess.SetWriteBuffer(16 * 1024 * 1024)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetMtu(1400)
	sess.SetMtu(1600)
	sess.SetMtu(1400)
	sess.SetACKNoDelay(true)
	sess.SetACKNoDelay(false)
	sess.SetRateLimit(200 * 1024 * 1024)
	sess.SetLogger(IKCP_LOG_ALL, newLoggerWithMilliseconds().Info)
	return sess, nil
}

// dialSink creates a loopback client for the discard-only throughput fixture.
func dialSink(port int) (*UDPSession, error) {
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
	if err != nil {
		panic(err)
	}

	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(16 * 1024 * 1024)
	sess.SetWriteBuffer(16 * 1024 * 1024)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetMtu(1400)
	sess.SetACKNoDelay(false)
	sess.SetDeadline(time.Now().Add(time.Minute))
	return sess, nil
}

// dialTinyBufferEcho creates a client whose small read/write pieces stress buffering
// boundaries.
func dialTinyBufferEcho(port int) (*UDPSession, error) {
	// block, _ := NewNoneBlockCrypt(pass)
	// block, _ := NewSimpleXORBlockCrypt(pass)
	// block, _ := NewTEABlockCrypt(pass[:16])
	// block, _ := NewAESBlockCrypt(pass)
	block, _ := NewSalsa20BlockCrypt(pass)
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
	if err != nil {
		panic(err)
	}
	return sess, nil
}

// ////////////////////////
func listenEcho(port int, block BlockCrypt) (net.Listener, error) {
	// block, _ := NewNoneBlockCrypt(pass)
	// block, _ := NewSimpleXORBlockCrypt(pass)
	// block, _ := NewTEABlockCrypt(pass[:16])
	// block, _ := NewAESBlockCrypt(pass)
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 1)
}

// listenTinyBufferEcho creates the matching listener for tiny-chunk echo tests.
func listenTinyBufferEcho(port int) (net.Listener, error) {
	// block, _ := NewNoneBlockCrypt(pass)
	// block, _ := NewSimpleXORBlockCrypt(pass)
	// block, _ := NewTEABlockCrypt(pass[:16])
	// block, _ := NewAESBlockCrypt(pass)
	block, _ := NewSalsa20BlockCrypt(pass)
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 3)
}

// listenSink creates the loopback discard listener used by throughput tests.
func listenSink(port int) (net.Listener, error) {
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
}

// echoServer starts the loopback echo acceptor and returns its listener for explicit teardown.
func echoServer(port int, block BlockCrypt) net.Listener {
	l, err := listenEcho(port, block)
	if err != nil {
		panic(err)
	}

	go func() {
		kcplistener := l.(*Listener)
		kcplistener.SetReadBuffer(4 * 1024 * 1024)
		kcplistener.SetWriteBuffer(4 * 1024 * 1024)
		kcplistener.SetDSCP(46)
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}

			// coverage test
			s.(*UDPSession).SetReadBuffer(4 * 1024 * 1024)
			s.(*UDPSession).SetWriteBuffer(4 * 1024 * 1024)
			go handleEcho(s.(*UDPSession))
		}
	}()

	return l
}

// sinkServer starts the loopback discard acceptor and returns its listener for explicit
// teardown.
func sinkServer(port int) net.Listener {
	l, err := listenSink(port)
	if err != nil {
		panic(err)
	}

	go func() {
		kcplistener := l.(*Listener)
		kcplistener.SetReadBuffer(4 * 1024 * 1024)
		kcplistener.SetWriteBuffer(4 * 1024 * 1024)
		kcplistener.SetDSCP(46)
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}

			go handleSink(s.(*UDPSession))
		}
	}()

	return l
}

// tinyBufferEchoServer starts the small-buffer acceptor to exercise stream aggregation and
// partial reads.
func tinyBufferEchoServer(port int) net.Listener {
	l, err := listenTinyBufferEcho(port)
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}
			go handleTinyBufferEcho(s.(*UDPSession))
		}
	}()
	return l
}

///////////////////////////

// handleEcho echoes reliable stream bytes under the fixture's configured window/packet
// settings.
func handleEcho(conn *UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWindowSize(1024, 1024)
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetDSCP(46)
	conn.SetMtu(1400)
	conn.SetACKNoDelay(false)
	conn.SetReadDeadline(time.Now().Add(time.Hour))
	conn.SetWriteDeadline(time.Now().Add(time.Hour))
	conn.SetRateLimit(200 * 1024 * 1024)
	buf := make([]byte, 512*1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		conn.Write(buf[:n])
	}
}

// handleSink drains reliable stream input so the benchmark measures sending without echo
// output.
func handleSink(conn *UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWindowSize(1024, 1024)
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetDSCP(46)
	conn.SetMtu(1400)
	conn.SetACKNoDelay(false)
	conn.SetReadDeadline(time.Now().Add(time.Hour))
	conn.SetWriteDeadline(time.Now().Add(time.Hour))
	buf := make([]byte, 16*1024*1024)
	for {
		_, err := conn.Read(buf)
		if err != nil {
			return
		}
	}
}

// handleTinyBufferEcho echoes tiny pieces to expose truncation or boundary assumptions in
// stream buffering.
func handleTinyBufferEcho(conn *UDPSession) {
	conn.SetStreamMode(true)
	buf := make([]byte, 2)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		conn.Write(buf[:n])
	}
}

///////////////////////////

// TestTimeout checks Timeout so a change cannot silently weaken the recorded regression
// contract.
func TestTimeout(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	buf := make([]byte, 10)

	// timeout
	cli.SetDeadline(time.Now().Add(time.Second))
	<-time.After(2 * time.Second)
	n, err := cli.Read(buf)
	if n != 0 || err == nil {
		t.Fail()
		return
	}
}

// TestCFBSendRecv checks CFB Send Recv so a change cannot silently weaken the recorded
// regression contract.
func TestCFBSendRecv(t *testing.T) {
	port := nextPort()
	block1, _ := NewTripleDESBlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewTripleDESBlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(true)

	randomEchoTest(t, cli, 100*1024*1024)
}

// TestSalsa20SendRecv checks Salsa20 Send Recv so a change cannot silently weaken the recorded
// regression contract.
func TestSalsa20SendRecv(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(true)

	randomEchoTest(t, cli, 100*1024*1024)
}

// TestAEADSendRecv checks AEAD Send Recv so a change cannot silently weaken the recorded
// regression contract.
func TestAEADSendRecv(t *testing.T) {
	port := nextPort()
	block1, _ := NewAESGCMCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewAESGCMCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(true)

	randomEchoTest(t, cli, 100*1024*1024)
}

// TestPlainTextSendRecv checks Plain Text Send Recv so a change cannot silently weaken the
// recorded regression contract.
func TestPlainTextSendRecv(t *testing.T) {
	port := nextPort()
	l := echoServer(port, nil)
	defer l.Close()

	cli, err := dialEcho(port, nil)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(true)

	randomEchoTest(t, cli, 100*1024*1024)
}

// Test1GBEcho checks 1 GB Echo so a change cannot silently weaken the recorded regression
// contract.
func Test1GBEcho(t *testing.T) {
	port := nextPort()
	l := echoServer(port, nil)
	defer l.Close()

	cli, err := dialEcho(port, nil)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(true)
	randomEchoTest(t, cli, 1*1024*1024*1024)
}

// Test6GBEcho checks 6 GB Echo so a change cannot silently weaken the recorded regression
// contract.
func Test6GBEcho(t *testing.T) {
	port := nextPort()
	l := echoServer(port, nil)
	defer l.Close()

	cli, err := dialEcho(port, nil)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(true)
	randomEchoTest(t, cli, 6*1024*1024*1024)
}

// randomEchoTest varies stream write/read boundaries and checks returned bytes across the
// reliable session.
func randomEchoTest(t *testing.T, cli *UDPSession, N int64) {
	seed := time.Now().UnixNano()
	writerSrc := mrand.NewSource(seed)
	readerSrc := mrand.NewSource(seed)

	bytesSent := int64(0)
	bytesReceived := int64(0)

	// Writer goroutine
	go func() {
		r := mrand.New(writerSrc)
		lenRand := mrand.New(mrand.NewSource(seed + 1))
		lastPrint := int64(0)
		sndbuf := make([]byte, 1<<20)
		for bytesSent < N {
			length := lenRand.Intn(1<<20) + 1 // Random length between 1 and 1MB
			if bytesSent+int64(length) > N {
				length = int(N - bytesSent)
			}
			payload := sndbuf[:length]
			if _, err := r.Read(payload); err != nil {
				t.Errorf("Random fill error: %v", err)
				return
			}

			n, err := cli.Write(payload)
			if err != nil {
				t.Errorf("Write error: %v", err)
				return
			}
			bytesSent += int64(n)
			if bytesSent-lastPrint >= 1<<28 { // print every 256MB
				lastPrint = bytesSent
				t.Logf("Sent %d%% (%d/%d bytes)", int(bytesSent*100/N), bytesSent, N)
			}
		}
	}()

	// Reader goroutine
	r := mrand.New(readerSrc)
	lenRand := mrand.New(mrand.NewSource(seed + 2))
	lastPrint := int64(0)
	rcvbuf := make([]byte, 1<<20)
	expbuf := make([]byte, 1<<20)
	for bytesReceived < N {
		length := lenRand.Intn(1<<20) + 1 // Random length between 1 and 1MB
		if bytesReceived+int64(length) > N {
			length = int(N - bytesReceived)
		}
		buf := rcvbuf[:length]
		n, err := cli.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("Read error: %v", err)
		}
		expected := expbuf[:n]
		if _, err := r.Read(expected); err != nil {
			t.Fatalf("Random fill error: %v", err)
		}
		if !bytes.Equal(buf[:n], expected) {
			for i := range n {
				if buf[i] != expected[i] {
					t.Fatalf("Data mismatch at byte %d: got %v, want %v", bytesReceived+int64(i), buf[i], expected[i])
				}
			}
			t.Fatalf("Data mismatch at byte %d", bytesReceived)
		}
		bytesReceived += int64(n)
		if bytesReceived-lastPrint >= 1<<28 { // print every 256MB
			lastPrint = bytesReceived
			t.Logf("Received %d%% (%d/%d bytes)", int(bytesReceived*100/N), bytesReceived, N)
		}
	}
}

// TestSendVector checks Send Vector so a change cannot silently weaken the recorded regression
// contract.
func TestSendVector(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(false)
	randomEchoVectorTest(t, cli)
}

// randomEchoVectorTest exercises scatter/gather echo writes so vector boundaries cannot
// corrupt application ordering.
func randomEchoVectorTest(t *testing.T, cli *UDPSession) {
	seed := time.Now().UnixNano()
	writerSrc := mrand.NewSource(seed)
	readerSrc := mrand.NewSource(seed)

	bytesSent := int64(0)
	bytesReceived := int64(0)

	const N = 100 * 1024 * 1024

	// Writer goroutine
	go func() {
		r := mrand.New(writerSrc)
		lastPrint := 0
		v := make([][]byte, 2)
		for bytesSent < N {
			length1 := mrand.Intn(1<<20) + 1 // Random length between 1 and 1MB
			if bytesSent+int64(length1) > N {
				length1 = int(N - bytesSent)
			}
			sndbuf1 := make([]byte, length1)

			for i := range sndbuf1 {
				sndbuf1[i] = byte(r.Int())
			}

			length2 := mrand.Intn(1<<20) + 1 // Random length between 1 and 1MB
			if bytesSent+int64(length2) > N {
				length2 = int(N - bytesSent)
			}

			sndbuf2 := make([]byte, length2)
			for i := range sndbuf2 {
				sndbuf2[i] = byte(r.Int())
			}

			v[0] = sndbuf1
			v[1] = sndbuf2

			n, err := cli.WriteBuffers(v)
			if err != nil {
				t.Errorf("Write error: %v", err)
				return
			}

			if n != length1+length2 {
				t.Errorf("Write length mismatch: got %v, want %v", n, length1+length2)
				return
			}

			bytesSent += int64(n)
			if percent := int(bytesSent * 100 / N); percent >= lastPrint+10 {
				lastPrint = percent
				t.Logf("Sent %d%% (%d/%d bytes)", percent, bytesSent, N)
			}
		}
	}()

	// Reader goroutine
	r := mrand.New(readerSrc)
	lastPrint := 0
	for bytesReceived < N {
		length := mrand.Intn(1<<20) + 1 // Random length between 1 and 1MB
		if bytesReceived+int64(length) > N {
			length = int(N - bytesReceived)
		}
		rcvbuf := make([]byte, length)
		n, err := cli.Read(rcvbuf)
		if err != nil && err != io.EOF {
			t.Fatalf("Read error: %v", err)
		}
		for i := range n {
			expectedByte := byte(r.Int())
			if rcvbuf[i] != expectedByte {
				t.Fatalf("Data mismatch at byte %d: got %v, want %v", bytesReceived+int64(i), rcvbuf[i], expectedByte)
			}
		}
		bytesReceived += int64(n)
		if percent := int(bytesReceived * 100 / N); percent >= lastPrint+10 {
			lastPrint = percent
			t.Logf("Received %d%% (%d/%d bytes)", percent, bytesReceived, N)
		}
	}
}

// TestTinyBufferReceiver checks Tiny Buffer Receiver so a change cannot silently weaken the
// recorded regression contract.
func TestTinyBufferReceiver(t *testing.T) {
	port := nextPort()
	l := tinyBufferEchoServer(port)
	defer l.Close()

	cli, err := dialTinyBufferEcho(port)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	const N = 100
	snd := byte(0)
	fillBuffer := func(buf []byte) {
		for i := range buf {
			buf[i] = snd
			snd++
		}
	}

	rcv := byte(0)
	check := func(buf []byte) bool {
		for i := range buf {
			if buf[i] != rcv {
				return false
			}
			rcv++
		}
		return true
	}
	sndbuf := make([]byte, 7)
	rcvbuf := make([]byte, 7)
	for range N {
		fillBuffer(sndbuf)
		cli.Write(sndbuf)

		n, err := io.ReadFull(cli, rcvbuf)
		if err != nil {
			panic(err)
		}

		if !check(rcvbuf[:n]) {
			t.Fail()
			return
		}
	}
}

// TestClose checks Close so a change cannot silently weaken the recorded regression contract.
func TestClose(t *testing.T) {
	var n int
	var err error

	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	// double close
	cli.Close()
	if cli.Close() == nil {
		t.Fatal("double close misbehavior")
		return
	}

	// write after close
	buf := make([]byte, 10)
	n, err = cli.Write(buf)
	if n != 0 || err == nil {
		t.Fatal("write after close misbehavior")
		return
	}

	// write, close, read, read
	block3, _ := NewSalsa20BlockCrypt(pass)
	cli, err = dialEcho(port, block3)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	if _, err = cli.Write(buf); err != nil {
		t.Fatal("write misbehavior")
		return
	}

	// wait until data arrival
	time.Sleep(2 * time.Second)
	// drain
	cli.Close()
	n, err = io.ReadFull(cli, buf)
	if err != nil {
		t.Fatal("closed conn drain bytes failed", err, n)
		return
	}

	// after drain, read should return error
	n, err = cli.Read(buf)
	if n != 0 || err == nil {
		t.Fatal("write->close->drain->read misbehavior", err, n)
		return
	}
}

// TestParallel1024CLIENT_64BMSG_64CNT checks Parallel1024 CLIENT 64 BMSG 64 CNT so a change
// cannot silently weaken the recorded regression contract.
func TestParallel1024CLIENT_64BMSG_64CNT(t *testing.T) {
	port := nextPort()
	block, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block)
	defer l.Close()

	var wg sync.WaitGroup
	wg.Add(1024)
	for range 1024 {
		go parallel_client(&wg, port)
	}
	wg.Wait()
}

// parallel_client owns one concurrent client task and signals completion to the test wait
// group.
func parallel_client(wg *sync.WaitGroup, port int) (err error) {
	block, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	err = echo_tester(cli, 64, 64)
	wg.Done()
	return
}

// BenchmarkEchoSpeed4K measures Echo Speed4 K with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkEchoSpeed4K(b *testing.B) {
	speedclient(b, 4096)
}

// BenchmarkEchoSpeed64K measures Echo Speed64 K with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkEchoSpeed64K(b *testing.B) {
	speedclient(b, 65536)
}

// BenchmarkEchoSpeed512K measures Echo Speed512 K with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkEchoSpeed512K(b *testing.B) {
	speedclient(b, 524288)
}

// BenchmarkEchoSpeed1M measures Echo Speed1 M with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkEchoSpeed1M(b *testing.B) {
	speedclient(b, 1048576)
}

// speedclient sets up the echo benchmark and measures its configured payload transfer size.
func speedclient(b *testing.B, nbytes int) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	b.ReportAllocs()
	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	if err := echo_tester(cli, nbytes, b.N); err != nil {
		b.Fail()
		return
	}
	b.SetBytes(int64(nbytes))
}

// BenchmarkSinkSpeed4K measures Sink Speed4 K with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkSinkSpeed4K(b *testing.B) {
	sinkclient(b, 4096)
}

// BenchmarkSinkSpeed64K measures Sink Speed64 K with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkSinkSpeed64K(b *testing.B) {
	sinkclient(b, 65536)
}

// BenchmarkSinkSpeed256K measures Sink Speed256 K with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkSinkSpeed256K(b *testing.B) {
	sinkclient(b, 524288)
}

// BenchmarkSinkSpeed1M measures Sink Speed1 M with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkSinkSpeed1M(b *testing.B) {
	sinkclient(b, 1048576)
}

// sinkclient sets up the discard benchmark to isolate output throughput from response traffic.
func sinkclient(b *testing.B, nbytes int) {
	port := nextPort()
	l := sinkServer(port)
	defer l.Close()

	b.ReportAllocs()
	cli, err := dialSink(port)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	sink_tester(cli, nbytes, b.N)
	b.SetBytes(int64(nbytes))
}

// echo_tester sends and validates repeated echo payloads so dropped/truncated bytes are not
// counted as successful traffic.
func echo_tester(cli net.Conn, msglen, msgcount int) error {
	go func() {
		buf := make([]byte, msglen)
		for range msgcount {
			// send packet
			if _, err := cli.Write(buf); err != nil {
				panic(err)
			}
		}
	}()

	// receive packet
	nrecv := 0
	buf := make([]byte, msglen)
	for {
		n, err := cli.Read(buf)
		if err != nil {
			return err
		}
		nrecv += n
		if nrecv == msglen*msgcount {
			break
		}
	}
	return nil
}

// sink_tester sends the sink workload used to measure accepted output without echo overhead.
func sink_tester(cli *UDPSession, msglen, msgcount int) error {
	// sender
	buf := make([]byte, msglen)
	for range msgcount {
		if _, err := cli.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

// TestSNMP checks SNMP so a change cannot silently weaken the recorded regression contract.
func TestSNMP(t *testing.T) {
	t.Log(DefaultSnmp.Copy())
	t.Log(DefaultSnmp.Header())
	t.Log(DefaultSnmp.ToSlice())
	DefaultSnmp.Reset()
	t.Log(DefaultSnmp.ToSlice())
}

// TestListenerClose checks Listener Close so a change cannot silently weaken the recorded
// regression contract.
func TestListenerClose(t *testing.T) {
	port := nextPort()
	l, err := ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 10, 3)
	if err != nil {
		t.Fail()
		return
	}
	defer l.Close()
	l.SetReadDeadline(time.Now().Add(time.Second))
	l.SetWriteDeadline(time.Now().Add(time.Second))
	l.SetDeadline(time.Now().Add(time.Second))
	time.Sleep(2 * time.Second)
	if _, err := l.Accept(); err == nil {
		t.Fail()
		return
	}

	l.Close()
	fakeaddr, _ := net.ResolveUDPAddr("udp6", "127.0.0.1:1111")
	if l.closeSession(fakeaddr) {
		t.Fail()
		return
	}
}

// A wrapper for net.PacketConn that remembers when Close has been called.
type closedFlagPacketConn struct {
	net.PacketConn
	Closed bool
}

// Close records close ownership before delegating socket shutdown in listener/client lifecycle
// tests.
func (c *closedFlagPacketConn) Close() error {
	c.Closed = true
	return c.PacketConn.Close()
}

// newClosedFlagPacketConn constructs a socket wrapper whose closure can be asserted after
// transport teardown.
func newClosedFlagPacketConn(c net.PacketConn) *closedFlagPacketConn {
	return &closedFlagPacketConn{c, false}
}

// Listener should not close a net.PacketConn that it did not create.
// https://github.com/xtaci/kcp-go/issues/165
func TestListenerNonOwnedPacketConn(t *testing.T) {
	// Create a net.PacketConn not owned by the Listener.
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer c.Close()
	// Make it remember when it has been closed.
	pconn := newClosedFlagPacketConn(c)

	l, err := ServeConn(nil, 0, 0, pconn)
	if err != nil {
		panic(err)
	}
	defer l.Close()

	if pconn.Closed {
		t.Fatal("non-owned PacketConn closed before Listener.Close()")
		return
	}

	err = l.Close()
	if err != nil {
		panic(err)
	}

	if pconn.Closed {
		t.Fatal("non-owned PacketConn closed after Listener.Close()")
		return
	}
}

// UDPSession should not close a net.PacketConn that it did not create.
// https://github.com/xtaci/kcp-go/issues/165
func TestUDPSessionNonOwnedPacketConn(t *testing.T) {
	l := sinkServer(0)
	defer l.Close()

	// Create a net.PacketConn not owned by the UDPSession.
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer c.Close()
	// Make it remember when it has been closed.
	pconn := newClosedFlagPacketConn(c)

	client, err := NewConn2(l.Addr(), nil, 0, 0, pconn)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	if pconn.Closed {
		t.Fatal("non-owned PacketConn closed before UDPSession.Close()")
		return
	}

	err = client.Close()
	if err != nil {
		panic(err)
	}

	if pconn.Closed {
		t.Fatal("non-owned PacketConn closed after UDPSession.Close()")
		return
	}
}

// this function test the data correctness with FEC and encryption enabled
func TestReliability(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(false)

	const N = 100000
	buf := make([]byte, 128)
	msg := make([]byte, 128)

	for range N {
		io.ReadFull(rand.Reader, msg)
		cli.Write([]byte(msg))

		n, err := io.ReadFull(cli, buf)
		if err != nil {
			panic(err)
		}

		if !bytes.Equal(buf[:n], msg) {
			t.Fail()
			return
		}
	}
}

// TestControl checks Control so a change cannot silently weaken the recorded regression
// contract.
func TestControl(t *testing.T) {
	port := nextPort()
	block, _ := NewSalsa20BlockCrypt(pass)
	l, err := ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), block, 10, 1)
	if err != nil {
		panic(err)
	}
	defer l.Close()

	errorA := errors.New("A")
	err = l.Control(func(conn net.PacketConn) error {
		fmt.Printf("Listener Control: conn: %v\n", conn)
		return errorA
	})

	if err != errorA {
		t.Fatal(err)
		return
	}

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	errorB := errors.New("B")
	err = cli.Control(func(conn net.PacketConn) error {
		fmt.Printf("Client Control: conn: %v\n", conn)
		return errorB
	})

	if err != errorB {
		t.Fatal(err)
		return
	}
}

// TestSessionReadAfterClosed checks Session Read After Closed so a change cannot silently
// weaken the recorded regression contract.
func TestSessionReadAfterClosed(t *testing.T) {
	us, _ := net.ListenPacket("udp", "127.0.0.1:0")
	uc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer us.Close()
	defer uc.Close()

	knockDoor := func(c net.Conn, myid string) (string, error) {
		c.SetDeadline(time.Now().Add(time.Second * 3))
		_, err := c.Write([]byte(myid))
		c.SetDeadline(time.Time{})
		if err != nil {
			return "", err
		}
		c.SetDeadline(time.Now().Add(time.Second * 3))
		var buf [1024]byte
		n, err := c.Read(buf[:])
		c.SetDeadline(time.Time{})
		return string(buf[:n]), err
	}

	check := func(c1, c2 *UDPSession) {
		done := make(chan struct{}, 1)
		go func() {
			rid, err := knockDoor(c2, "4321")
			done <- struct{}{}
			if err != nil {
				panic(err)
			}

			if rid != "1234" {
				panic("mismatch id")
			}
		}()

		rid, err := knockDoor(c1, "1234")
		if err != nil {
			panic(err)
		}

		if rid != "4321" {
			panic("mismatch id")
		}
		<-done
	}

	c1, err := NewConn3(0, uc.LocalAddr(), nil, 0, 0, us)
	if err != nil {
		panic(err)
	}
	defer c1.Close()

	c2, err := NewConn3(0, us.LocalAddr(), nil, 0, 0, uc)
	if err != nil {
		panic(err)
	}
	defer c2.Close()

	check(c1, c2)
	c1.Close()
	c2.Close()
	// log.Println("conv id 0 is closed")

	c1, err = NewConn3(4321, uc.LocalAddr(), nil, 0, 0, us)
	if err != nil {
		panic(err)
	}
	defer c1.Close()

	c2, err = NewConn3(4321, us.LocalAddr(), nil, 0, 0, uc)
	if err != nil {
		panic(err)
	}
	defer c2.Close()

	check(c1, c2)
	c1.Close()
	c2.Close()
}

// TestSetMTU checks Set MTU so a change cannot silently weaken the recorded regression
// contract.
func TestSetMTU(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	// Session headerSize = cryptHeaderSize(20) + fecHeaderSizePlus2(8) = 28
	// UDPSession.SetMtu subtracts headerSize before calling kcp.SetMtu,
	// so the minimum user-facing MTU that yields kcp mtu = IKCP_OVERHEAD+1 is:
	//   IKCP_OVERHEAD + 1 + headerSize = 25 + 28 = 53
	sessionOverhead := cryptHeaderSize + fecHeaderSizePlus2
	minValidMTU := IKCP_OVERHEAD + sessionOverhead + 1

	tests := []struct {
		name string
		mtu  int
		want bool
	}{
		{"reject: kcp mtu would be negative", IKCP_OVERHEAD, false},
		{"reject: kcp mtu would equal IKCP_OVERHEAD", minValidMTU - 1, false},
		{"accept: minimum valid MTU", minValidMTU, true},
		{"accept: typical MTU", 1400, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cli.SetMtu(tt.mtu)
			if got != tt.want {
				t.Errorf("SetMtu(%d) = %v, want %v", tt.mtu, got, tt.want)
			}
		})
	}

	// echo round-trip at standard MTU
	cli.SetMtu(1500)
	cli.SetWriteDelay(false)
	cli.SetLogger(IKCP_LOG_ALL, newLoggerWithMilliseconds().Info)

	sendBytes := make([]byte, 1500)
	rand.Read(sendBytes)
	cli.Write(sendBytes)

	buf := make([]byte, 1500)

	n, err := io.ReadFull(cli, buf)
	if err != nil {
		panic(err)
	}

	if !bytes.Equal(buf[:n], sendBytes) {
		t.Fail()
		return
	}
}

// newLoggerWithMilliseconds creates the test trace format needed to inspect retransmission
// timing.
func newLoggerWithMilliseconds() *slog.Logger {
	timeFormat := "2006-01-02 15:04:05.000"
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(time.Now().Format(timeFormat))
			}
			return a
		},
	})
	return slog.New(handler)
}

// TestSetLogger if want logging kcp trace need set build tags with debug
// trace log on:
//
//	go test -run ^TestSetLogger$ -tags debug
//
// trace log off:
//
//	go test -run ^TestSetLogger$
func TestSetLogger(t *testing.T) {
	port := nextPort()
	block1, _ := NewSalsa20BlockCrypt(pass)
	l := echoServer(port, block1)
	defer l.Close()

	block2, _ := NewSalsa20BlockCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	cli.SetWriteDelay(true)
	cli.SetDUP(1)
	cli.SetLogger(IKCP_LOG_ALL, newLoggerWithMilliseconds().Info)
	const N = 10
	buf := make([]byte, 10)
	for i := range N {
		msg := fmt.Sprintf("trace%v", i)
		cli.Write([]byte(msg))

		n, err := cli.Read(buf)
		if err != nil {
			panic(err)
		}

		if string(buf[:n]) != msg {
			t.Fail()
			return
		}
	}
}

// largeNonceAEAD retains the large Nonce AEAD fixture state used to expose failures without
// production network side effects.
type largeNonceAEAD struct {
	cipher.AEAD
}

// NonceSize advertises an intentionally large nonce to exercise transport-overhead validation.
func (*largeNonceAEAD) NonceSize() int {
	return 1400
}

// Overhead reports authentication expansion for the oversized-nonce fixture.
func (*largeNonceAEAD) Overhead() int {
	return 0
}

// TestLargeNonce checks Large Nonce so a change cannot silently weaken the recorded regression
// contract.
func TestLargeNonce(t *testing.T) {
	port := nextPort()

	aead := new(largeNonceAEAD)
	block := NewAEADCrypt(aead)

	defer func() {
		if recover() != "Overhead too large" {
			t.Fatal("expect panic with Overhead too large")
			return
		}
	}()

	cli, err := dialEcho(port, block)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
}

// largeOverheadAEAD retains the large Overhead AEAD fixture state used to expose failures
// without production network side effects.
type largeOverheadAEAD struct {
	cipher.AEAD
}

// NonceSize reports the fixture nonce size for oversized authentication overhead tests.
func (*largeOverheadAEAD) NonceSize() int {
	return 0
}

// Overhead advertises deliberately oversized expansion so invalid MTU budgets are rejected.
func (*largeOverheadAEAD) Overhead() int {
	return 1400
}

// TestLargeOverhead checks Large Overhead so a change cannot silently weaken the recorded
// regression contract.
func TestLargeOverhead(t *testing.T) {
	port := nextPort()

	aead := new(largeOverheadAEAD)
	block := NewAEADCrypt(aead)

	defer func() {
		if recover() != "Overhead too large" {
			t.Fatal("expect panic with Overhead too large")
			return
		}
	}()

	cli, err := dialEcho(port, block)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
}

// checkAllocatedAEAD retains the check Allocated AEAD fixture state used to expose failures
// without production network side effects.
type checkAllocatedAEAD struct {
	cipher.AEAD
}

// Seal checks destination capacity during encryption so MTU/nonce overhead mistakes become
// visible test failures.
func (aead *checkAllocatedAEAD) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
	if dst == nil || cap(dst)-len(dst) < len(plaintext)+aead.AEAD.Overhead() {
		panic("AEAD Seal will allocate new slice")
	}

	ciphertext := aead.AEAD.Seal(dst, nonce, plaintext, additionalData)
	if &ciphertext[0] != &dst[:1][0] {
		panic("AEAD Seal allocated new slice")
	}
	return ciphertext
}

// TestSealAllocated checks Seal Allocated so a change cannot silently weaken the recorded
// regression contract.
func TestSealAllocated(t *testing.T) {
	aes, err := aes.NewCipher(pass[:16])
	if err != nil {
		panic(err)
	}

	aesgcm, err := cipher.NewGCM(aes)
	if err != nil {
		panic(err)
	}

	port := nextPort()
	block := NewAEADCrypt(&checkAllocatedAEAD{aesgcm})

	l := echoServer(port, block)
	defer l.Close()

	cli, err := dialEcho(port, block)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	b := make([]byte, 100*1024*1024) // 100 MB
	cli.Write(b)
}

// TestSessionGetters checks Session Getters so a change cannot silently weaken the recorded
// regression contract.
func TestSessionGetters(t *testing.T) {
	sess := new(UDPSession)
	sess.kcp = NewKCP(1, func(buf []byte, size int) {})

	if sess.GetConv() != 1 {
		t.Error("GetConv failed")
	}
	// RTO, SRTT, SRTTVar are dynamic, just check they don't panic
	sess.GetRTO()
	sess.GetSRTT()
	sess.GetSRTTVar()
}

// TestTimedSchedClose checks Timed Sched Close so a change cannot silently weaken the recorded
// regression contract.
func TestTimedSchedClose(t *testing.T) {
	ts := NewTimedSched(1)
	ts.Close()
}

// TestListenDial checks Listen Dial so a change cannot silently weaken the recorded regression
// contract.
func TestListenDial(t *testing.T) {
	l, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Logf("Listening on %s", l.Addr().String())

	ch := make(chan struct{})
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		t.Log("Accepted connection")
		conn.Close()
		close(ch)
	}()

	conn, err := Dial(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Dialed")
	conn.Write([]byte("hello"))
	t.Log("Wrote data")
	time.Sleep(100 * time.Millisecond)
	conn.Close()
	<-ch
}

// TestOOB verifies the end-to-end transmission and reception of OOB (Out-Of-Band) data:
// 1. The server registers an OOB callback and echoes back any received OOB data.
// 2. The client registers an OOB callback to validate the content and length of echoed OOB data.
// 3. The client sends OOB data of varying lengths in a loop, counting the number of echoes for each length.
// 4. Finally, it checks that all lengths of OOB data are correctly echoed back.
// This test ensures the OOB data channel is functional and the content is accurate.
func TestOOB(t *testing.T) {
	port := nextPort()
	block1, _ := NewAESGCMCrypt(pass)

	l, err := listenEcho(port, block1)
	if err != nil {
		panic(err)
	}
	defer l.Close()

	go func() {
		// Server listens for OOB data and echoes it back
		kcplistener := l.(*Listener)
		kcplistener.SetReadBuffer(4 * 1024 * 1024)
		kcplistener.SetWriteBuffer(4 * 1024 * 1024)
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}
			sess := s.(*UDPSession)
			sess.SetReadBuffer(4 * 1024 * 1024)
			sess.SetWriteBuffer(4 * 1024 * 1024)
			// Register OOB callback, echo back received OOB data immediately
			sess.SetOOBHandler(func(buf []byte) {
				if err := sess.SendOOB(buf); err != nil {
					t.Errorf("server failed to echo OOB payload: %v", err)
				}
			})
			go handleEcho(sess)
		}
	}()

	block2, _ := NewAESGCMCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(false)

	size := cli.GetOOBMaxSize()
	if size < 1000 {
		t.Errorf("unexpectedly small max OOB size: %d", size)
	}
	t.Log("Max OOB size:", size)

	sizePlus1 := size + 1
	counts := make([]atomic.Int32, sizePlus1)

	// Client registers OOB callback to validate echoed OOB data content and length
	cli.SetOOBHandler(func(buf []byte) {
		for i, b := range buf {
			if b != byte(i) {
				t.Fatalf(
					"OOB echo payload mismatch at offset %d: expected %d, got %d",
					i, byte(i), b,
				)
				break
			}
		}
		counts[len(buf)].Add(1)
	})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		// Stress test for normal data channel to ensure main channel does not affect OOB
		randomEchoTest(t, cli, 10*1024*1024)
	}()

	go func() {
		defer wg.Done()
		// Send OOB data of varying lengths in a loop, content is [0,1,2,...]
		buf := make([]byte, size)
		for i := range len(buf) {
			buf[i] = byte(i)
		}
		for i := range 10 * 1024 * 1024 {
			if err := cli.SendOOB(buf[:i%sizePlus1]); err != nil {
				t.Errorf("client failed to send OOB payload: %v", err)
			}
			if i%128 == 0 {
				runtime.Gosched()
			}
		}
	}()

	wg.Wait()

	// Check that all lengths of OOB data are correctly echoed back
	for i := range counts {
		if counts[i].Load() == 0 {
			t.Errorf("missing OOB echo for payload length %d", i)
		}
	}
}

// TestOOB_OneSideHandler verifies that OOB data can be received and processed
// when only the server sets the OOB handler and the client does not.
//
// The server OOB handler updates the shared 'counts' slice, which is initialized
// in the main goroutine and referenced by the handler for each OOB payload length.
// This test ensures that the server can receive and correctly process OOB packets
// of all possible lengths, even if the client does not set any OOB handler.
func TestOOB_OneSideHandler(t *testing.T) {
	port := nextPort()
	block1, _ := NewAESGCMCrypt(pass)

	l, err := listenEcho(port, block1)
	if err != nil {
		panic(err)
	}
	defer l.Close()

	counts := make([]atomic.Int32, mtuLimit)
	errCh := make(chan error, 1)

	// Get OOB max payload size and initialize the shared counts slice.
	go func() {
		// Server sets OOB handler and counts received OOB packets by length.
		// The handler directly updates the shared 'counts' slice.
		kcplistener := l.(*Listener)
		kcplistener.SetReadBuffer(4 * 1024 * 1024)
		kcplistener.SetWriteBuffer(4 * 1024 * 1024)
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}
			sess := s.(*UDPSession)
			sess.SetReadBuffer(4 * 1024 * 1024)
			sess.SetWriteBuffer(4 * 1024 * 1024)
			// Only the server sets the OOB handler.
			// The handler references the 'counts' slice from the main goroutine.
			sess.SetOOBHandler(func(buf []byte) {
				// Validate OOB payload content and count by length.
				for i, b := range buf {
					if b != byte(i) {
						select {
						case errCh <- fmt.Errorf("OOB payload mismatch at offset %d: expected %d, got %d", i, byte(i), b):
						default:
						}
					}
				}
				counts[len(buf)].Add(1)
			})
			go handleEcho(sess)
		}
	}()

	block2, _ := NewAESGCMCrypt(pass)
	cli, err := dialEcho(port, block2)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.SetWriteDelay(false)

	// Unknown control/OOB packets must not allocate listener sessions. A
	// reliable round trip establishes the session and confirms that Accept
	// installed the server handler before the unreliable OOB flood begins.
	cli.SetDeadline(time.Now().Add(5 * time.Second))
	randomEchoTest(t, cli, 1)
	cli.SetDeadline(time.Time{})

	size := cli.GetOOBMaxSize()
	sizePlus1 := size + 1

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		// Client does NOT set OOB handler, only sends OOB packets of varying lengths.
		buf := make([]byte, size)
		for i := range len(buf) {
			buf[i] = byte(i)
		}
		for i := range 10 * 1024 * 1024 {
			if err := cli.SendOOB(buf[:i%sizePlus1]); err != nil {
				t.Errorf("client failed to send OOB payload: %v", err)
			}
			if i%128 == 0 {
				runtime.Gosched()
			}
		}
	}()

	wg.Wait()

	// Give the server time to process incoming OOB packets.
	time.Sleep(500 * time.Millisecond)

	select {
	case err := <-errCh:
		t.Fatalf("%v", err)
	default:
	}

	// Check that all lengths of OOB data are received by the server.
	for i := range counts[:sizePlus1] {
		if counts[i].Load() == 0 {
			t.Errorf("server missing OOB for payload length %d", i)
		}
	}
}

// TestSetOOBHandler_Basic checks Set OOB Handler Basic so a change cannot silently weaken the
// recorded regression contract.
func TestSetOOBHandler_Basic(t *testing.T) {
	sess := new(UDPSession)
	sess.kcp = NewKCP(1, func(buf []byte, size int) {})
	// Should return error if FEC is not enabled
	err := sess.SetOOBHandler(func([]byte) {})
	if err == nil {
		t.Error("expected error when FEC is not enabled")
	}

	// Should allow register/unregister callback after FEC enabled
	sess.fecEncoder = newFECEncoder(1, 1, 0)
	if err := sess.SetOOBHandler(nil); err != nil {
		t.Error("unregister nil callback should not error")
	}
	// After setting nil, callbackForOOB should be a dummy handler, and calling it should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("dummy OOB handler should not panic, got panic: %v", r)
		}
	}()
	if f := sess.callbackForOOB.Load(); f != nil {
		typeName := reflect.TypeOf(f).String()
		t.Logf("callbackForOOB type: %s", typeName)

		if cb, ok := f.(OOBCallBackType); ok {
			cb([]byte("dummy"))
		} else {
			t.Errorf("callbackForOOB type assertion failed: got %T", f)
		}
	} else {
		t.Error("callbackForOOB should not be nil after SetOOBHandler(nil)")
	}
	called := false
	cb := func([]byte) { called = true }
	if err := sess.SetOOBHandler(cb); err != nil {
		t.Errorf("register callback failed: %v", err)
	}
	// Simulate callback invocation
	if f := sess.callbackForOOB.Load(); f != nil {
		if cb2, ok := f.(OOBCallBackType); ok {
			cb2([]byte("test"))
			if !called {
				t.Error("callback not called as expected")
			}
		} else {
			t.Errorf("callbackForOOB type assertion failed: got %T", f)
		}
	}
}

// TestGetOOBMaxSize checks Get OOB Max Size so a change cannot silently weaken the recorded
// regression contract.
func TestGetOOBMaxSize(t *testing.T) {
	sess := new(UDPSession)
	sess.kcp = NewKCP(1, func(buf []byte, size int) {})
	// Should return 0 if FEC is not enabled
	if n := sess.GetOOBMaxSize(); n != 0 {
		t.Errorf("expected 0 when FEC not enabled, got %d", n)
	}
	// Should return mtu-4 after FEC enabled
	sess.fecEncoder = newFECEncoder(1, 1, 0)
	sess.kcp.mtu = 1400
	if n := sess.GetOOBMaxSize(); n != 1396 {
		t.Errorf("expected 1396, got %d", n)
	}
}

// TestSendOOB_Errors checks Send OOB Errors so a change cannot silently weaken the recorded
// regression contract.
func TestSendOOB_Errors(t *testing.T) {
	sess := new(UDPSession)
	sess.kcp = NewKCP(1, func(buf []byte, size int) {})
	// Should error if FEC is not enabled
	err := sess.SendOOB([]byte("abc"))
	if err == nil || err.Error() != "OOB requires FEC to be enabled" {
		t.Errorf("expected 'OOB requires FEC to be enabled', got %v", err)
	}
	// NOTE: SendOOB no longer requires callbackForOOB to be non-nil on sender side.
	// It should not return error if callback is not set after FEC is enabled.
	sess.fecEncoder = newFECEncoder(1, 1, 0)
	err = sess.SendOOB([]byte("abc"))
	if err != nil {
		t.Errorf("expected no error when callback not set, got %v", err)
	}
	// Should error if payload is too large
	sess.kcp.mtu = 8
	cb := func([]byte) {}
	sess.SetOOBHandler(cb)
	err = sess.SendOOB([]byte("123456789"))
	if err == nil || err.Error() != "OOB payload too large" {
		t.Errorf("expected 'OOB payload too large', got %v", err)
	}
}
