package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestLoadRequiresCompleteResponse prevents a successful HTTP status from
// masking truncated application traffic in capacity qualification results.
func TestLoadRequiresCompleteResponse(t *testing.T) {
	for _, complete := range []bool{true, false} {
		t.Run(map[bool]string{true: "complete", false: "truncated"}[complete], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := "super-paqet benchmark\n"
				if !complete {
					body = "short"
				}
				io.WriteString(w, body)
			}))
			defer server.Close()
			result := captureLoad(t, "", server.URL)
			if complete && (result["requests"] == 0 || result["successful_workers"] != 2) {
				t.Fatal("every worker must complete a full response:", result)
			}
			if !complete && (result["requests"] != 0 || result["errors"] == 0 || result["successful_workers"] != 0) {
				t.Fatal("truncated responses counted as success:", result)
			}
		})
	}
}

// TestLoadUsesExplicitProxy checks that application-path qualification actually
// crosses the proxy rather than silently connecting directly to its target.
func TestLoadUsesExplicitProxy(t *testing.T) {
	var routed atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.RequestURI, "http://qualification.invalid/") {
			routed.Add(1)
		}
		io.WriteString(w, "super-paqet benchmark\n")
	}))
	defer proxy.Close()
	result := captureLoad(t, proxy.URL, "http://qualification.invalid/")
	if result["requests"] == 0 || routed.Load() == 0 {
		t.Fatal("explicit proxy was not used:", result)
	}
}

// TestLoadPauseHonorsCancellation prevents a configured customer pause from
// retaining load-generator workers after the qualification deadline.
func TestLoadPauseHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitForLoad(ctx, time.Hour) {
		t.Fatal("canceled workload resumed its request loop")
	}
}

// captureLoad collects the public JSON result of a bounded real HTTP exercise.
// These tests run serially because stdout belongs to the CLI process.
func captureLoad(t *testing.T, proxy, target string) map[string]float64 {
	return captureTimedLoad(t, context.Background(), proxy, target, 100*time.Millisecond, 0)
}

// captureTimedLoad retains the real HTTP/JSON boundary while selecting startup
// and measurement durations; stdout ownership keeps these tests serial.
func captureTimedLoad(t *testing.T, ctx context.Context, proxy, target string, duration, warmup time.Duration) map[string]float64 {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original; w.Close() }()
	load(ctx, "http", "", 2, duration, proxy, target, 22, 0, warmup)
	w.Close()
	var result map[string]float64
	// The mode field is a string; decode numeric fields without assuming all
	// future benchmark metadata has the same type.
	var raw map[string]any
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	result = make(map[string]float64)
	for k, v := range raw {
		if number, ok := v.(float64); ok {
			result[k] = number
		}
	}
	return result
}

// TestLoadWarmupKeepsStartupFailures exercises a real truncated first reply:
// warmup must not hide it, while measured counts exclude valid startup traffic.
func TestLoadWarmupKeepsStartupFailures(t *testing.T) {
	var served atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if served.Add(1) == 1 {
			io.WriteString(w, "short")
			return
		}
		io.WriteString(w, "super-paqet benchmark\n")
	}))
	defer server.Close()
	result := captureTimedLoad(t, context.Background(), "", server.URL, 100*time.Millisecond, 150*time.Millisecond)
	if result["errors"] != 1 || result["warmup_errors"] != 1 || result["warmup_requests"] == 0 || result["requests"] == 0 {
		t.Fatal("startup failure hidden or warmup/measurement traffic missing:", result)
	}
	if result["total_seconds"]-result["seconds"] < .149 || result["seconds"] < .09 || result["seconds"] > .2 {
		t.Fatal("measurement denominator includes the warmup:", result)
	}
	if result["warmup_bytes"] == 0 || result["successful_workers"] != 2 {
		t.Fatal("startup traffic or measured worker verification lost:", result)
	}
}

// TestLoadCanceledDuringWarmup prevents early cancellation from becoming a
// successful steady-state throughput sample with a negative denominator.
func TestLoadCanceledDuringWarmup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "super-paqet benchmark\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result := captureTimedLoad(t, ctx, "", server.URL, 100*time.Millisecond, 150*time.Millisecond)
	if result["seconds"] != 0 || result["requests"] != 0 || result["requests_per_second"] != 0 || result["successful_workers"] != 0 {
		t.Fatal("canceled startup became a completed measurement:", result)
	}
}

// TestHoldTargetHonorsIntegritySize checks the real idle-target HTTP path so a
// body-size mismatch cannot be mistaken for tunnel truncation in scale tests.
func TestHoldTargetHonorsIntegritySize(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	bulk := make([]byte, 65536)
	for i := range bulk {
		bulk[i] = byte(i)
	}
	go serveHoldConn(right, bulk)
	left.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(left)
	for _, size := range []int{1, 1025, 1048576} {
		request, err := http.NewRequest("GET", "http://benchmark.invalid/bulk?bytes="+strconv.Itoa(size), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = request.Write(left); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(reader, request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || len(body) != size {
			t.Fatalf("wanted %d bytes, got %d: %v", size, len(body), err)
		}
		for i, v := range body {
			if v != byte(i) {
				t.Fatalf("incorrect pattern at %d", i)
			}
		}
	}
}
