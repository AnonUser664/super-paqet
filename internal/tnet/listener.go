package tnet

import (
	"net"

	"paqet/internal/conf"
)

type Listener interface {
	RegisterClient(net.Addr, uint32)
	SetClientTCPFSession(net.Addr, uint32, []conf.TCPF)
	DeleteClientSession(net.Addr, uint32)
	Accept() (Conn, error)
	Close() error
	Addr() net.Addr
	SetClientTCPF(addr net.Addr, f []conf.TCPF)
	DeleteClientTCPF(addr net.Addr)
}
