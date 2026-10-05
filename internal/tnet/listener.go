// File listener.go: defines incoming carrier admission and generation-owned outer flag
// configuration.

package tnet

import (
	"net"

	"paqet/internal/conf"
)

// Listener defines incoming carrier acceptance and generation-owned outer flag setup/teardown.
type Listener interface {
	// Records the current owner generation before peer flag setup is applied.
	RegisterClient(net.Addr, uint32)
	// Updates flags only for the still-current conversation owner.
	SetClientTCPFSession(net.Addr, uint32, []conf.TCPF)
	// Removes state only when teardown still belongs to the current generation.
	DeleteClientSession(net.Addr, uint32)
	// Accepts a logical carrier; the returned connection does not own the shared listener
	// socket.
	Accept() (Conn, error)
	// Releases owned resources; accepted carrier wrappers never own the listener packet socket.
	Close() error
	// Reports the listener endpoint used by the underlying packet transport.
	Addr() net.Addr
	// Compatibility peer flag update delegated to shared encoder state.
	SetClientTCPF(addr net.Addr, f []conf.TCPF)
	// Compatibility flag removal that preserves the default outer cycle.
	DeleteClientTCPF(addr net.Addr)
}
