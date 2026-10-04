//go:build linux

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type blockedLogWriter struct {
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedLogWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func TestDiagnosticQueueDoesNotBlockForwarding(t *testing.T) {
	c := LogConfig{Level: "debug"}
	if err := c.prepare(); err != nil {
		t.Fatal(err)
	}
	w := &blockedLogWriter{entered: make(chan struct{}), release: make(chan struct{})}
	d := newDiagnostics(c, w)
	d.logger.Debug("first")
	<-w.entered
	done := make(chan struct{})
	go func() {
		for i := 0; i < 4096; i++ {
			d.logger.Debug("burst", "id", i)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("logging backpressured forwarding")
	}
	if d.dropped.Load() == 0 {
		t.Fatal("overflow was not observable")
	}
	close(w.release)
	d.close()
}

func TestStructuredDiagnosticsLevelAndFields(t *testing.T) {
	c := LogConfig{Level: "info"}
	if err := c.prepare(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	d := newDiagnostics(c, &out)
	d.logger.Debug("hidden")
	d.logger.With("conv", uint32(12)).InfoContext(context.Background(), "session.connected", "remote", "192.0.2.1:29999")
	d.close()
	var row map[string]any
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["msg"] != "session.connected" || row["conv"] != float64(12) {
		t.Fatalf("lost correlation: %v", row)
	}
}

func TestInvalidLogConfiguration(t *testing.T) {
	for _, c := range []LogConfig{{Level: "verbose"}, {Format: "yaml"}, {Interval: "0s"}} {
		if c.prepare() == nil {
			t.Fatal("invalid log config accepted")
		}
	}
}
