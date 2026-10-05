//go:build linux

// File batch_linux.go: exposes batch I/O and driver drop counters to KCP without switching the
// raw transport to UDP.

package socket

import "golang.org/x/net/ipv4"

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
