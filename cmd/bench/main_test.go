package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// captureLoad collects the public JSON result of a bounded real HTTP exercise.
// These tests run serially because stdout belongs to the CLI process.
func captureLoad(t *testing.T, proxy, target string) map[string]float64 {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original; w.Close() }()
	load(context.Background(), "http", "", 2, 100*time.Millisecond, proxy, target, 22)
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
