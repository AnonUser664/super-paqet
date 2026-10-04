//go:build linux

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

type LogConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	Interval   string `yaml:"interval"`
	FlowSample uint64 `yaml:"flow_sample"`
	duration   time.Duration
	level      slog.Level
}

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

type logItem struct {
	handler slog.Handler
	record  slog.Record
}
type diagnostics struct {
	logger  *slog.Logger
	queue   chan logItem
	done    chan struct{}
	mu      sync.RWMutex
	closed  bool
	dropped atomic.Uint64
}
type queuedHandler struct {
	backend slog.Handler
	owner   *diagnostics
}

func (h *queuedHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.backend.Enabled(ctx, l)
}
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
func (h *queuedHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &queuedHandler{h.backend.WithAttrs(a), h.owner}
}
func (h *queuedHandler) WithGroup(name string) slog.Handler {
	return &queuedHandler{h.backend.WithGroup(name), h.owner}
}
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

func (e *Engine) flowTrace() uint64 {
	if e.diagnostics == nil || !e.diagnostics.logger.Enabled(e.ctx, slog.LevelDebug) {
		return 0
	}
	id := e.flowIDs.Add(1)
	if id%e.cfg.Log.FlowSample != 0 {
		return 0
	}
	return id
}

func (e *Engine) log() *slog.Logger {
	if e.diagnostics == nil {
		return slog.Default()
	}
	return e.diagnostics.logger
}
