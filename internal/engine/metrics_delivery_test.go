//go:build linux

// File metrics_delivery_test.go verifies that an accepted carrier exposes
// delivery evidence independently of outgoing peer and shared-socket counters.
package engine

import (
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMetricsAcceptedCarrierDelivery performs real mux delivery and observes
// ACK progress, rather than treating an attempted write as proof of delivery.
func TestMetricsAcceptedCarrierDelivery(t *testing.T) {
	e := reloadFixture(t)
	server, client := acceptedMigrationPair(t, migrationServer(t), "127.0.0.1")
	e.addPassive(server)
	out, err := server.Session.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	in, err := client.Session.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out.SetDeadline(time.Now().Add(3 * time.Second))
	in.SetDeadline(time.Now().Add(3 * time.Second))
	payload := []byte("accepted carrier delivery evidence")
	if _, err := out.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(in, got); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("delivery %q: %v", got, err)
	}
	until := time.Now().Add(3 * time.Second)
	for server.UDPSession.TransportStats().AckedBytes < uint64(len(payload)) && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	recorder := httptest.NewRecorder()
	e.metrics(recorder, httptest.NewRequest("GET", "/metrics", nil))
	labels := fmt.Sprintf("conv=\"%d\",remote=%s", server.UDPSession.GetConv(), strconv.Quote(server.RemoteAddr().String()))
	values := map[string]uint64{}
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			if n, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				values[fields[0]] = n
			}
		}
	}
	for _, name := range []string{"acked_bytes_total", "received_bytes_total", "pending_bytes", "retransmitted_segments_total", "spurious_retransmissions_total", "rto_ms", "rttvar_ms", "send_queued", "receive_queued", "receive_reordered", "pipeline_queued"} {
		key := "super_paqet_session_" + name + "{" + labels + "}"
		n, ok := values[key]
		if !ok {
			t.Fatalf("accepted session missing %s", key)
		}
		if name == "acked_bytes_total" && n < uint64(len(payload)) {
			t.Fatalf("delivered payload has no ACK evidence: %d", n)
		}
		if name == "received_bytes_total" && n == 0 {
			t.Fatal("accepted bootstrap has no inbound delivery evidence")
		}
	}
}
