// File dial.go: constructs an owned outgoing raw socket, KCP conversation and client mux
// session with failure cleanup.

package kcp

import (
	"fmt"
	"net"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"

	"paqet/internal/conf"
	"paqet/internal/socket"
	"paqet/internal/tnet"
)

// Dial builds an owned outgoing raw/KCP/mux stack and closes previously created layers if
// construction fails.
func Dial(addr *net.UDPAddr, cfg *conf.KCP, netCfg conf.Network) (tnet.Conn, error) {
	nCfg := netCfg
	packetConn, err := socket.New(&nCfg)
	if err != nil {
		return nil, fmt.Errorf("kcp: failed to create packetconn: %w", err)
	}

	conn, err := kcp.NewConn(addr.String(), cfg.Block, cfg.Dshard, cfg.Pshard, packetConn)
	if err != nil {
		packetConn.Close()
		return nil, fmt.Errorf("kcp: failed to dial connection: %w", err)
	}
	if err := aplConf(conn, cfg); err != nil {
		conn.Close()
		packetConn.Close()
		return nil, err
	}

	sess, err := smux.Client(conn, smuxConf(cfg, conn))
	if err != nil {
		conn.Close()
		packetConn.Close()
		return nil, fmt.Errorf("kcp: failed to create smux session: %w", err)
	}

	return &Conn{PacketConn: packetConn, UDPSession: conn, Session: sess}, nil
}
