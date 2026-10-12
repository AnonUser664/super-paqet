//go:build linux

// File batch_linux.go: exposes batch I/O and driver drop counters to KCP without switching the
// raw transport to UDP.

package socket

import (
	kcplib "github.com/xtaci/kcp-go/v5"
	"golang.org/x/net/ipv4"
)

// NewSessionWriter is resolved by a Linux listener once per carrier/generation.
// Pcap and ordinary client paths retain their existing descriptor ownership.
func (c *PacketConn) NewSessionWriter() (kcplib.SessionWriter, error) {
	// The private-output prototype failed its mixed-load CPU/latency gates.
	// Ordinary release builds retain the shared physical writer; experiments
	// must request the explicit build tag and qualify their exact executable.
	if !privateOutputEnabled || c.raw == nil {
		return nil, nil
	}
	return c.raw.(*rawPacket).newSessionWriter()
}

// SessionWriterFailures exposes safe output-factory fallbacks without treating
// construction failure as loss of an existing carrier or shared receive socket.
func (c *PacketConn) SessionWriterFailures() uint64 {
	if c.raw == nil {
		return 0
	}
	return c.raw.(*rawPacket).writerFailures.Load()
}

// TXDrops reports socket-scoped transmit queue loss for either driver rather than treating
// attempted bytes as delivered bytes.
func (c *PacketConn) TXDrops() uint64 {
	if c.raw != nil {
		return c.raw.(*rawPacket).txDrops.Load()
	}
	if c.sendHandle != nil {
		return c.sendHandle.txDrops.Load()
	}
	return 0
}

// SharePacketEncoder retains the baseline's single timestamp/sequence counter
// and per-client flag state across a server's packet receive workers.
func (c *PacketConn) SharePacketEncoder(first *PacketConn) {
	if c.raw != nil && first.raw != nil {
		c.raw.(*rawPacket).send = first.raw.(*rawPacket).send
	}
}

// PacketStats returns cumulative packet capture counters from the active driver without
// changing carrier ownership.
func (c *PacketConn) PacketStats() (uint64, uint64) {
	if c.raw != nil {
		return c.raw.(*rawPacket).packetStats()
	}
	return 0, 0
}

// WriteBatch delegates Linux packet batching or falls back to ordered individual pcap
// injections.
func (c *PacketConn) WriteBatch(ms []ipv4.Message, flags int) (int, error) {
	if c.raw != nil {
		return c.raw.(*rawPacket).WriteBatch(ms, flags)
	}
	for i := range ms {
		n, err := c.WriteTo(ms[i].Buffers[0], ms[i].Addr)
		ms[i].N = n
		if err != nil {
			return i, err
		}
	}
	return len(ms), nil
}

// WriteBatchWithDrops is optional KCP feedback for this batch only. Report
// wholly rejected datagrams as consumed loss, preserving reliable retry rather
// than turning transient queue pressure into a fatal socket error.
func (c *PacketConn) WriteBatchWithDrops(ms []ipv4.Message, flags int) (int, int, error) {
	if c.raw != nil {
		return c.raw.(*rawPacket).writeBatchWithDrops(ms, flags)
	}
	// The pcap path retains legacy injection behavior. Its driver cannot yet
	// supply per-call soft-loss attribution, so never substitute shared totals.
	n, err := c.WriteBatch(ms, flags)
	return n, 0, err
}

// ReadBatch delegates raw receive batches or fills one pcap message through the same packet
// interface.
func (c *PacketConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	if c.raw != nil {
		return c.raw.(*rawPacket).ReadBatch(ms, flags)
	}
	if len(ms) == 0 {
		return 0, nil
	}
	n, addr, err := c.ReadFrom(ms[0].Buffers[0])
	ms[0].N = n
	ms[0].Addr = addr
	if err != nil {
		return 0, err
	}
	return 1, nil
}

// TXQueueRetries reports pre-send recovery attempts without conflating them
// with emitted retransmissions or unresolved datagram loss. Pcap retains its
// existing per-packet drop behavior and has no queue-retry counter.
func (c *PacketConn) TXQueueRetries() uint64 {
	if c.raw != nil {
		return c.raw.(*rawPacket).txRetries.Load()
	}
	return 0
}
