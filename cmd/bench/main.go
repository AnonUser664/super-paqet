// bench is a standalone load generator. Its resource use is separate from the tunnel.
// File main.go: provides targets, load generators and integrity checks used by isolated
// qualification rather than the production tunnel.

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// main dispatches CLI work and returns failures as a nonzero process outcome for automation.
func main() {
	mode := flag.String("mode", "http", "serve, http, bulk, hold, or verify")
	addr := flag.String("addr", "127.0.0.1:18080", "address; comma separated addresses for hold")
	workers := flag.Int("workers", 32, "concurrent requests/connection ramp workers")
	duration := flag.Duration("duration", 10*time.Second, "measurement duration")
	count := flag.Int("connections", 1000, "hold connection count")
	verifySize := flag.Int("verify-size", 16777216, "integrity-check payload bytes")
	proxy := flag.String("proxy", "", "optional HTTP proxy for authenticated application-path load")
	requestURL := flag.String("url", "", "optional absolute HTTP target for load modes")
	expectedBytes := flag.Int64("response-bytes", 0, "required response length for a custom load URL")
	requestGap := flag.Duration("request-gap", 0, "optional pause between requests per worker; zero saturates the path")
	flag.Parse()
	if *workers < 1 || *duration <= 0 || *count < 1 || *requestGap < 0 {
		panic("workers, duration and connections must be positive")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch *mode {
	case "udp-serve":
		a, err := net.ResolveUDPAddr("udp", *addr)
		if err != nil {
			panic(err)
		}
		c, err := net.ListenUDP("udp", a)
		if err != nil {
			panic(err)
		}
		defer c.Close()
		go func() { <-ctx.Done(); c.Close() }()
		buf := make([]byte, 65535)
		for {
			n, a, err := c.ReadFromUDPAddrPort(buf)
			if err != nil {
				break
			}
			if _, err := c.WriteToUDPAddrPort(buf[:n], a); err != nil {
				panic(err)
			}
		}
	case "udp-verify":
		c, err := net.Dial("udp", *addr)
		if err != nil {
			panic(err)
		}
		defer c.Close()
		buf := make([]byte, 65535)
		for _, size := range []int{0, 1, 1300, 4096, 65507, 2, 0} {
			want := make([]byte, size)
			for i := range want {
				want[i] = byte(i * 31)
			}
			c.SetDeadline(time.Now().Add(15 * time.Second))
			if _, err := c.Write(want); err != nil {
				panic(err)
			}
			n, err := c.Read(buf)
			if err != nil {
				panic(err)
			}
			if !bytes.Equal(buf[:n], want) {
				panic("UDP datagram corruption/boundary failure")
			}
		}
		emit(map[string]any{"udp_datagrams_verified": 7, "udp_max_bytes": 65507})
	case "half-serve":
		l, err := net.Listen("tcp", *addr)
		if err != nil {
			panic(err)
		}
		defer l.Close()
		go func() { <-ctx.Done(); l.Close() }()
		for {
			c, err := l.Accept()
			if err != nil {
				break
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(15 * time.Second))
				_, err := io.ReadAll(io.LimitReader(c, 1<<20))
				if err == nil {
					c.Write(bytes.Repeat([]byte("response"), 32768))
				}
			}()
		}
	case "half-verify":
		c, err := net.Dial("tcp", *addr)
		if err != nil {
			panic(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(15 * time.Second))
		if _, err := c.Write([]byte("request")); err != nil {
			panic(err)
		}
		if err := c.(*net.TCPConn).CloseWrite(); err != nil {
			panic(err)
		}
		got, err := io.ReadAll(c)
		if err != nil {
			panic(err)
		}
		if !bytes.Equal(got, bytes.Repeat([]byte("response"), 32768)) {
			panic("half-close response truncated")
		}
		emit(map[string]any{"half_close_verified_bytes": len(got)})
	case "hold-serve":
		serveHold(ctx, *addr)
	case "serve":
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "super-paqet benchmark\n") })
		buf := make([]byte, 64*1024)
		for i := range buf {
			buf[i] = byte(i)
		}
		mux.HandleFunc("/bulk", func(w http.ResponseWriter, r *http.Request) {
			size := 16777216
			if text := r.URL.Query().Get("bytes"); text != "" {
				n, err := strconv.Atoi(text)
				if err != nil || n < 1 || n > size {
					http.Error(w, "invalid size", 400)
					return
				}
				size = n
			}
			w.Header().Set("Content-Length", strconv.Itoa(size))
			for left := size; left > 0; left -= len(buf) {
				if _, err := w.Write(buf[:min(left, len(buf))]); err != nil {
					return
				}
			}
		})
		mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
			n, err := io.Copy(io.Discard, r.Body)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			fmt.Fprintf(w, "%d", n)
		})
		s := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() { <-ctx.Done(); s.Close() }()
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	case "hold":
		hold(ctx, strings.Split(*addr, ","), *workers, *count, *duration)
	case "http", "http-churn", "bulk":
		load(ctx, *mode, *addr, *workers, *duration, *proxy, *requestURL, *expectedBytes, *requestGap)
	case "verify":
		tr := &http.Transport{DisableKeepAlives: true}
		defer tr.CloseIdleConnections()
		c := &http.Client{Transport: tr, Timeout: 30 * time.Second}
		r, err := c.Get("http://" + *addr + "/bulk?bytes=" + strconv.Itoa(*verifySize))
		if err != nil {
			panic(err)
		}
		defer r.Body.Close()
		b := make([]byte, 32768)
		offset := 0
		for {
			n, err := r.Body.Read(b)
			for i, v := range b[:n] {
				if v != byte(offset+i) {
					panic("data corruption")
				}
			}
			offset += n
			if err == io.EOF {
				break
			}
			if err != nil {
				panic(err)
			}
		}
		if offset != *verifySize {
			panic("truncated transfer")
		}
		emit(map[string]any{"verified_bytes": offset})
	default:
		panic("unknown mode")
	}
}

// Minimal HTTP responder keeps target memory from dominating the idle test.
// Bulk and request-rate tests use the net/http server above.
func serveHold(ctx context.Context, addr string) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	go func() { <-ctx.Done(); l.Close() }()
	bulk := make([]byte, 65536)
	for i := range bulk {
		bulk[i] = byte(i)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			var buf [512]byte
			for {
				n := 0
				for !bytes.Contains(buf[:n], []byte("\r\n\r\n")) {
					if n == len(buf) {
						return
					}
					k, err := conn.Read(buf[n:])
					if err != nil {
						return
					}
					n += k
				}
				if bytes.HasPrefix(buf[:n], []byte("GET /bulk")) {
					if _, err := io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 16777216\r\n\r\n"); err != nil {
						return
					}
					for j := 0; j < 256; j++ {
						if _, err := conn.Write(bulk); err != nil {
							return
						}
					}
				} else {
					if _, err := io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 22\r\n\r\nsuper-paqet benchmark\n"); err != nil {
						return
					}
				}
			}
		}()
	}
}

// emit writes one machine-readable benchmark result so qualification does not infer success
// from logs alone.
func emit(v any) {
	if err := json.NewEncoder(os.Stdout).Encode(v); err != nil {
		panic(err)
	}
}

// load runs bounded concurrent HTTP/bulk work and separates deadline cancellation from
// unexpected request failure.
func load(parent context.Context, mode, addr string, workers int, duration time.Duration, proxy, requestURL string, expectedBytes int64, requestGaps ...time.Duration) {
	ctx, cancel := context.WithTimeout(parent, duration)
	defer cancel()
	var gap time.Duration
	if len(requestGaps) > 0 {
		gap = requestGaps[0]
	}
	tr := &http.Transport{MaxIdleConns: workers, MaxIdleConnsPerHost: workers, MaxConnsPerHost: workers, DisableCompression: true}
	// One HTTP/1 connection per worker makes concurrency meaningful; HTTP/2 could
	// otherwise hide thousands of requests inside a few application connections.
	tr.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	if proxy != "" {
		p, err := url.Parse(proxy)
		if err != nil || p.Scheme != "http" || p.Host == "" {
			panic("proxy must be an absolute HTTP URL")
		}
		tr.Proxy = http.ProxyURL(p)
	}
	tr.DisableKeepAlives = mode == "http-churn"
	defer tr.CloseIdleConnections()
	// A stalled worker must become a recorded failure during a long soak, rather
	// than waiting until the workload deadline and looking like normal cleanup.
	c := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	path := "/"
	if mode == "bulk" {
		path = "/bulk?bytes=1048576"
	}
	if requestURL == "" {
		requestURL = "http://" + addr + path
		expectedBytes = 22
		if mode == "bulk" {
			expectedBytes = 1048576
		}
	} else {
		u, err := url.Parse(requestURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || expectedBytes < 1 {
			panic("custom URL requires an absolute HTTP(S) URL and positive response-bytes")
		}
	}
	var requests, failed, canceled, bytes atomic.Int64
	var successfulWorkers atomic.Int64
	var buckets [32]atomic.Int64
	// Millisecond buckets resolve WAN latency changes that disappear inside
	// logarithmic bounds; the logarithmic histogram still covers long stalls.
	var millisecondBuckets [2001]atomic.Int64
	var latencySum atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			completed := false
			defer func() {
				if completed {
					successfulWorkers.Add(1)
				}
			}()
			// Stagger a rate-limited cohort deterministically instead of creating
			// synchronized handshake bursts that misrepresent steady customers.
			if gap > 0 && !waitForLoad(ctx, time.Duration(i)*gap/time.Duration(workers)) {
				return
			}
			attempted := false
			for ctx.Err() == nil {
				// Failed responses observe the same rate bound as successes.
				if attempted && gap > 0 && !waitForLoad(ctx, gap) {
					return
				}
				attempted = true
				t0 := time.Now()
				r, _ := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
				resp, err := c.Do(r)
				if err != nil {
					if ctx.Err() == nil {
						failed.Add(1)
					} else {
						canceled.Add(1)
					}
					continue
				}
				n, err := io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				bytes.Add(n)
				if err != nil || resp.StatusCode != 200 || n != expectedBytes {
					if ctx.Err() == nil {
						failed.Add(1)
					} else {
						canceled.Add(1)
					}
					continue
				}
				requests.Add(1)
				completed = true
				us := time.Since(t0).Microseconds()
				latencySum.Add(us)
				millisecondBuckets[min(int(us/1000), len(millisecondBuckets)-1)].Add(1)
				bucket := 0
				for us > 1 && bucket < len(buckets)-1 {
					us >>= 1
					bucket++
				}
				buckets[bucket].Add(1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	percentile := func(p float64) int64 {
		threshold := int64(float64(requests.Load()) * p)
		var n int64
		for i := 0; i < len(millisecondBuckets)-1; i++ {
			n += millisecondBuckets[i].Load()
			if n > threshold {
				return int64(i+1) * 1000
			}
		}
		n = 0
		for i := range buckets {
			n += buckets[i].Load()
			if n > threshold {
				return 1 << (i + 1)
			}
		}
		return 0
	}
	emit(map[string]any{"mode": mode, "workers": workers, "successful_workers": successfulWorkers.Load(), "request_gap_seconds": gap.Seconds(), "seconds": elapsed, "requests": requests.Load(), "errors": failed.Load(), "canceled_requests": canceled.Load(), "bytes": bytes.Load(), "goodput_gbps": float64(bytes.Load()) * 8 / elapsed / 1e9, "requests_per_second": float64(requests.Load()) / elapsed, "mean_latency_us": float64(latencySum.Load()) / float64(max(1, requests.Load())), "p50_us_upper": percentile(.50), "p99_us_upper": percentile(.99)})
}

// waitForLoad makes cohort staggering and pauses interruptible at the workload
// deadline so canceled requests are not confused with application failures.
func waitForLoad(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// hold ramps and retains TCP forwards, then verifies every held socket instead of counting
// possibly dead descriptors.
func hold(parent context.Context, addresses []string, workers, count int, duration time.Duration) {
	var conns []net.Conn
	var mu sync.Mutex
	var next, failed atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := net.Dialer{Timeout: 15 * time.Second}
			for {
				n := int(next.Add(1) - 1)
				if n >= count || parent.Err() != nil {
					return
				}
				c, err := d.DialContext(parent, "tcp", addresses[n%len(addresses)])
				if err == nil {
					c.SetDeadline(time.Now().Add(20 * time.Second))
					_, err = io.WriteString(c, "GET / HTTP/1.1\r\nHost: bench\r\n\r\n")
					if err == nil { // A response proves the server-side connection exists too.
						buf := make([]byte, 4096)
						var response string
						for !strings.Contains(response, "super-paqet benchmark\n") {
							var k int
							k, err = c.Read(buf)
							response += string(buf[:k])
							if err != nil {
								break
							}
						}
					}
					c.SetDeadline(time.Time{})
				}
				if err != nil {
					failed.Add(1)
					if c != nil {
						c.Close()
					}
					continue
				}
				mu.Lock()
				conns = append(conns, c)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	emit(map[string]any{"phase": "established", "connections": len(conns), "errors": failed.Load(), "ramp_seconds": time.Since(start).Seconds()})
	timer := time.NewTimer(duration)
	select {
	case <-timer.C:
	case <-parent.Done():
	}
	timer.Stop()
	// Verify every held connection, including the complete response body.
	// Sampling can miss isolated local TCP timeouts at this scale.
	var checked, verified atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(checked.Add(1) - 1)
				if i >= len(conns) {
					return
				}
				c := conns[i]
				c.SetDeadline(time.Now().Add(10 * time.Second))
				_, err := io.WriteString(c, "GET / HTTP/1.1\r\nHost: bench\r\n\r\n")
				var response string
				var b [512]byte
				for err == nil && !strings.Contains(response, "super-paqet benchmark\n") {
					var n int
					n, err = c.Read(b[:])
					response += string(b[:n])
					if len(response) > 4096 {
						err = fmt.Errorf("invalid hold verification response")
					}
				}
				if err != nil {
					failed.Add(1)
				} else {
					verified.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	emit(map[string]any{"phase": "verified", "connections": len(conns), "verified": verified.Load(), "errors": failed.Load()})
	for _, c := range conns {
		c.Close()
	}
	emit(map[string]any{"phase": "closed", "connections": len(conns), "errors": failed.Load()})
}
