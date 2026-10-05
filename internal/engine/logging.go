//go:build linux

// File logging.go: decouples diagnostics from forwarding using a bounded queue, explicit drops
// and sampled lifecycle events.

package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// LogConfig controls structured output and sampling without putting sink latency on forwarding
// goroutines.
type LogConfig struct {
	// Configured diagnostic minimum level.
	Level string `yaml:"level"`
	// Structured JSON or text output, validated before sink construction.
	Format string `yaml:"format"`
	// Summary/sample cadence string, not a packet flush or retry interval.
	Interval string `yaml:"interval"`
	// Lifecycle sampling divisor; it is not a per-flow timer.
	FlowSample uint64 `yaml:"flow_sample"`
	// Parsed diagnostic cadence used by the shared observer ticker.
	duration time.Duration
	// Parsed slog threshold retained separately from the public string.
	level slog.Level
}

// prepare validates output format/interval and applies sampling defaults before diagnostics
// start.
func (c *LogConfig) prepare() error {
	if c.Level == "" {
		c.Level = "info"
	}
	if err := c.level.UnmarshalText([]byte(c.Level)); err != nil {
		return fmt.Errorf("log.level: %w", err)
	}
	if c.Format == "" {
		c.Format = "json"
	}
	if c.Format != "json" && c.Format != "text" {
		return fmt.Errorf("log.format must be json or text")
	}
	if c.Interval == "" {
		c.Interval = "1s"
		if c.level > slog.LevelDebug {
			c.Interval = "10s"
		}
	}
	var err error
	c.duration, err = time.ParseDuration(c.Interval)
	if err != nil || c.duration < 100*time.Millisecond {
		return fmt.Errorf("log.interval must be at least 100ms")
	}
	if c.FlowSample == 0 {
		c.FlowSample = 1000
	}
	return nil
}

// logItem retains a cloned record and handler until the single asynchronous output consumer
// writes it.
type logItem struct {
	// Output destination/context retained with a cloned queued record.
	handler slog.Handler
	// Cloned log data safe to retain after the forwarding call returns.
	record slog.Record
}

// diagnostics owns bounded logging, output drain and dropped-record accounting for one engine
// run.
type diagnostics struct {
	// Engine diagnostic entry point backed by the bounded queue.
	logger *slog.Logger
	// Bounded asynchronous records; a slow sink causes counted drops rather than forwarding
	// backpressure.
	queue chan logItem
	// Signals that the sole output consumer has finished draining queued records.
	done chan struct{}
	// Protects accepting/closing the log queue while producers remain nonblocking.
	mu sync.RWMutex
	// Rejects future work after lifecycle shutdown has begun.
	closed bool
	// Counts log overflow/output failures without blocking packet forwarding.
	dropped atomic.Uint64
}

// queuedHandler adapts slog handlers to nonblocking queued delivery while retaining grouping
// and attributes.
type queuedHandler struct {
	// Underlying formatting handler; grouped/attributed wrappers keep the same queue owner.
	backend slog.Handler
	// Shared diagnostics queue/lifecycle owner, not a second output consumer.
	owner *diagnostics
}

// Enabled delegates level filtering so disabled records do not enter the asynchronous queue.
func (h *queuedHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.backend.Enabled(ctx, l)
}

// Handle copies a record into a bounded queue without blocking the forwarding goroutine;
// overflow is counted.
func (h *queuedHandler) Handle(ctx context.Context, r slog.Record) error {
	h.owner.mu.RLock()
	defer h.owner.mu.RUnlock()
	if h.owner.closed {
		return nil
	}
	select {
	case h.owner.queue <- logItem{h.backend, r.Clone()}:
	default:
		h.owner.dropped.Add(1)
	}
	return nil
}

// WithAttrs retains the same queue owner when contextual attributes are added to a logger.
func (h *queuedHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &queuedHandler{h.backend.WithAttrs(a), h.owner}
}

// WithGroup retains asynchronous delivery while nesting structured log attributes.
func (h *queuedHandler) WithGroup(name string) slog.Handler {
	return &queuedHandler{h.backend.WithGroup(name), h.owner}
}

// newDiagnostics starts the sole output consumer so a slow log sink cannot directly stall data
// forwarding.
func newDiagnostics(cfg LogConfig, out io.Writer) *diagnostics {
	if out == nil {
		out = os.Stderr
	}
	options := &slog.HandlerOptions{Level: cfg.level}
	var backend slog.Handler = slog.NewJSONHandler(out, options)
	if cfg.Format == "text" {
		backend = slog.NewTextHandler(out, options)
	}
	d := &diagnostics{queue: make(chan logItem, 1024), done: make(chan struct{})}
	d.logger = slog.New(&queuedHandler{backend, d})
	go func() {
		defer close(d.done)
		for item := range d.queue {
			if err := item.handler.Handle(context.Background(), item.record); err != nil {
				d.dropped.Add(1)
			}
		}
	}()
	return d
}

// close stops accepting records and bounds shutdown drain time even if output is slow.
func (d *diagnostics) close() {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.queue)
	}
	d.mu.Unlock()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-d.done:
	case <-timer.C:
	}
}

// flowTrace allocates correlation IDs only when debug flow tracing is enabled.
func (e *Engine) flowTrace() uint64 {
	if e.diagnostics == nil || !e.diagnostics.logger.Enabled(e.ctx, slog.LevelDebug) {
		return 0
	}
	id := e.flowIDs.Add(1)
	return id
}

// traceFlow selects lifecycle records deterministically from the configured sampling divisor.
func (e *Engine) traceFlow(id uint64) bool { return id != 0 && id%e.cfg.Log.FlowSample == 0 }

// log returns the engine logger, with a standard fallback for small tests or uninitialized
// diagnostics.
func (e *Engine) log() *slog.Logger {
	if e.diagnostics == nil {
		return slog.Default()
	}
	return e.diagnostics.logger
}
