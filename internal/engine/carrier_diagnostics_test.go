//go:build linux

// File carrier_diagnostics_test.go checks warning evidence and ownership bounds
// without production traffic, raw sockets or bearer capability disclosure.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"paqet/internal/conf"
)

// TestCarrierRecoveryWarningExplainsFallback verifies the preserving/no-carrier
// case is visible at warn level even when no negotiated session can be moved.
func TestCarrierRecoveryWarningExplainsFallback(t *testing.T) {
	e, p, old := carrierRecoveryFixture(t)
	ep := *p.configuration()
	ep.PathRecovery.PreserveConnections = true
	e.resources["peer/broken"].spec.endpoint = ep
	p.settings.Store(&ep)
	cfg := LogConfig{Level: "warn"}
	if err := cfg.prepare(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	e.diagnostics = newDiagnostics(cfg, &out)
	e.pathProbe = func(_ context.Context, _ *peer) error { return nil }
	e.recoverCarrier("broken", p, 0, old)
	e.diagnostics.close()
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row["msg"] == "path.recovered" {
			found = true
			if row["preservation_reason"] != "no_live_carrier" || row["connections_preserved"] != false || row["old_close_reason"] != "" {
				t.Fatal(row)
			}
		}
	}
	if !found {
		t.Fatal("fallback was silent at warning level")
	}
}

// TestCarrierEndRetainsIOCause demonstrates cleanup cannot turn an actual
// carrier input failure into an unexplained local closure warning.
func TestCarrierEndRetainsIOCause(t *testing.T) {
	e := reloadFixture(t)
	l := migrationServer(t)
	c := acceptedMigrationConn(t, l, "127.0.0.1")
	c.UDPSession.Close()
	until := time.Now().Add(time.Second)
	for c.Session.EndCause().Reason == "" && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	c.Session.Close()
	cfg := LogConfig{Level: "warn"}
	if err := cfg.prepare(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	e.diagnostics = newDiagnostics(cfg, &out)
	e.logCarrierEnd(c)
	e.diagnostics.close()
	var row map[string]any
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["msg"] != "session.closed" || row["reason"] != "transport_read" || row["error"] == nil {
		t.Fatal(row)
	}
}

// TestRecoveryGraceEndpointOwnershipAndReload limits grace to supported roles
// and verifies a mux-lifetime edit cannot sneak into a reliability-only update.
func TestRecoveryGraceEndpointOwnershipAndReload(t *testing.T) {
	for _, test := range []struct{ listener, shared, preserve, fec, valid bool }{
		{true, true, false, false, true}, {true, false, false, false, false},
		{false, false, true, false, true}, {false, false, false, false, false},
		{false, false, true, true, false},
	} {
		ep := Endpoint{SharedSource: test.shared, PathRecovery: PathRecoveryConfig{PreserveConnections: test.preserve}, KCP: conf.KCP{SmuxRecoveryGrace: 60}}
		if test.fec {
			ep.KCP.Dshard, ep.KCP.Pshard = 10, 3
		}
		if (ep.prepareRecoveryGrace(test.listener) == nil) != test.valid {
			t.Fatal(test)
		}
	}
	a := resourceSpec{kind: "peer", endpoint: Endpoint{KCP: conf.KCP{SmuxRecoveryGrace: 0}}}
	b := a
	b.endpoint.KCP.SmuxRecoveryGrace = 60
	if sameResource(a, b) || canUpdateReliability(a, b) {
		t.Fatal("grace edit failed to replace mux contract")
	}
}
