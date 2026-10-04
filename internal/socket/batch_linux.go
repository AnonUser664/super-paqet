//go:build linux

package socket

import "golang.org/x/net/ipv4"

func (c *PacketConn) TXDrops() uint64 {
	if c.raw != nil {
		return c.raw.(*rawPacket).txDrops.Load()
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

func (c *PacketConn) PacketStats() (uint64, uint64) {
	if c.raw != nil {
		return c.raw.(*rawPacket).packetStats()
	}
	return 0, 0
}

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
