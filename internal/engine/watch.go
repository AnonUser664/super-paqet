//go:build linux

// File watch.go: detects changes by content rather than inode/mtime, so in-place
// edits, atomic replacement and symlink rotation all use the same reload path.
package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// maxConfigBytes bounds watcher allocation even if the path is replaced with a
// large file; 16 MiB permits large route tables without unbounded config memory.
const maxConfigBytes = 16 << 20

// ReloadConfig controls edit detection independently of transport retry timing.
type ReloadConfig struct {
	// Nil enables automatic polling; SIGHUP can explicitly reload when false.
	Enabled *bool `yaml:"enabled"`
	// Poll interval string; it bounds detection cost for unchanged files.
	Interval string `yaml:"interval"`
	// Stable-content grace for editors performing several consecutive writes.
	Debounce string `yaml:"debounce"`
	// Parsed interval and grace retained in immutable runtime settings.
	interval, debounce time.Duration
}

// prepare rejects unreasonable polling budgets before starting a watcher.
func (r *ReloadConfig) prepare() error {
	if r.Interval == "" {
		r.Interval = "250ms"
	}
	if r.Debounce == "" {
		r.Debounce = "500ms"
	}
	var err error
	r.interval, err = time.ParseDuration(r.Interval)
	if err != nil || r.interval < 50*time.Millisecond || r.interval > time.Minute {
		return fmt.Errorf("reload.interval must be 50ms..1m")
	}
	r.debounce, err = time.ParseDuration(r.Debounce)
	if err != nil || r.debounce < 0 || r.debounce > time.Minute {
		return fmt.Errorf("reload.debounce must be 0s..1m")
	}
	return nil
}

// readConfigBytes reads a bounded snapshot. The watcher validates this exact
// snapshot, not a later reread that might contain half of another edit.
func readConfigBytes(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("configuration must be a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxConfigBytes {
		return nil, fmt.Errorf("configuration exceeds 16 MiB")
	}
	return b, nil
}

// RunFile is the CLI lifecycle entry point: it loads once, then watches the same
// path until engine cancellation. Run remains useful for prepared-config tests.
func RunFile(ctx context.Context, path string) error {
	b, err := readConfigBytes(path)
	if err != nil {
		return err
	}
	cfg, err := parseConfig(b)
	if err != nil {
		return err
	}
	return run(ctx, cfg, func(e *Engine) { e.watchConfig(path, b) })
}

// watchConfig serializes edit application, retries temporary bind failures, and
// rate-limits repeated rejection logs. Invalid syntax is never retried until its
// bytes change or an operator sends SIGHUP; transient resource failures retry.
func (e *Engine) watchConfig(path string, initial []byte) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	ticker := time.NewTicker(e.current().Reload.interval)
	defer ticker.Stop()
	applied := sha256.Sum256(initial)
	var pending []byte
	var changed time.Time
	var attempted [32]byte
	var attemptedSet bool
	var retryAt time.Time
	var readError string
	for {
		force := false
		select {
		case <-e.ctx.Done():
			return
		case <-hup:
			force = true
		case <-ticker.C:
			if enabled := e.current().Reload.Enabled; enabled != nil && !*enabled {
				continue
			}
		}
		ticker.Reset(e.current().Reload.interval)
		b, err := readConfigBytes(path)
		if err != nil {
			if err.Error() != readError {
				readError = err.Error()
				e.reloadRejected.Add(1)
				e.log().Warn("config.read_failed", "path", path, "error", err, "revision", e.revision.Load())
			}
			pending = nil
			changed = time.Time{}
			continue
		}
		readError = ""
		hash := sha256.Sum256(b)
		if hash == applied && !force {
			pending = nil
			attemptedSet = false
			continue
		}
		now := time.Now()
		if !bytes.Equal(pending, b) {
			pending = append(pending[:0], b...)
			changed = now
			attemptedSet = false
			retryAt = time.Time{}
		}
		if !force && now.Sub(changed) < e.current().Reload.debounce {
			continue
		}
		if attemptedSet && attempted == hash && !force && (retryAt.IsZero() || now.Before(retryAt)) {
			continue
		}
		attempted, attemptedSet = hash, true
		c, err := parseConfig(b)
		if err != nil {
			retryAt = time.Time{}
			e.reloadRejected.Add(1)
			// YAML errors can quote source text containing inline secrets. Operators
			// get strict diagnostics via --check; daemon logs never print YAML excerpts.
			e.log().Warn("config.rejected", "phase", "validation", "reason", "invalid configuration; run --check for details", "path", path, "revision", e.revision.Load())
			continue
		}
		e.log().Info("config.detected", "path", path, "trigger", map[bool]string{false: "file", true: "SIGHUP"}[force], "revision", e.revision.Load())
		if err = e.apply(c); err != nil {
			retryAt = now.Add(5 * time.Second)
			e.reloadRejected.Add(1)
			e.log().Warn("config.rejected", "phase", "resources", "error", err, "retry_in", "5s", "revision", e.revision.Load())
			continue
		}
		applied = hash
		pending = nil
		attemptedSet = false
		retryAt = time.Time{}
	}
}
