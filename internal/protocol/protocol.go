// File protocol.go: frames bounded inner opening/control messages; application reliability
// remains with the surrounding KCP stream.

package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"paqet/internal/conf"
	"paqet/internal/tnet"
)

// PType names bounded inner control request kinds without changing the outer transport.
type PType = byte

const (
	// MAGIC identifies an inner control record before its body is interpreted.
	MAGIC byte = 0x50
	// VERSION selects the retained control-header version, separate from enterprise request type
	// semantics.
	VERSION byte = 0x01

	// PPING requests a carrier response without dialing an application target.
	PPING PType = 0x01
	// PPONG acknowledges a control ping; it does not prove sustained target delivery.
	PPONG PType = 0x02
	// PTCPF carries the requested peer outer flag cycle inside the reliable carrier.
	PTCPF PType = 0x03
	// PTCP names the legacy TCP request type; the enterprise dispatcher uses the directional-
	// close type instead.
	PTCP PType = 0x04
	// PUDP names the legacy UDP request type; enterprise UDP requires explicit datagram framing.
	PUDP PType = 0x05
	// Enterprise streams require directional FIN and framed UDP respectively.
	PTCP2 PType = 0x06
	// PUDP2 selects enterprise length-framed UDP relay while retaining reliable carrier delivery.
	PUDP2 PType = 0x07
	// PMTOKEN negotiates a random session capability, without modifying packets.
	PMTOKEN PType = 0x08
	// PMCHECK verifies a candidate can adopt a live session before client movement.
	PMCHECK PType = 0x09
	// PMMOVE commits an idempotent physical transport generation.
	PMMOVE PType = 0x0a
	// PMREPLY carries an explicit migration result and matching capability/generation.
	PMREPLY PType = 0x0b
	// PTCP3/PUDP3 retain a bounded opening identity across carrier retries.
	PTCP3 PType = 0x0c
	PUDP3 PType = 0x0d
)

const (
	headerLen = 5 // MAGIC, VERSION, TYPE, LENGTH(2)
	// maxHostLen bounds transmitted target names to limit control allocation and reject malformed
	// input.
	maxHostLen = 253
	// maxTCPFCount bounds the remote outer flag cycle carried by one control request.
	maxTCPFCount = 64
	// maxBodyLen caps declared inner control storage before reading attacker-provided lengths.
	maxBodyLen = 4096
	// maxPort bounds target ports to their 16-bit wire representation.
	maxPort = 0xFFFF
)

const (
	// bFIN assigns the FIN bit in inner peer flag setup; its wire value must remain stable.
	bFIN = 1 << 0
	// bSYN assigns the SYN bit in inner peer flag setup; its wire value must remain stable.
	bSYN = 1 << 1
	// bRST assigns the RST bit in inner peer flag setup; its wire value must remain stable.
	bRST = 1 << 2
	// bPSH assigns the PSH bit in inner peer flag setup; its wire value must remain stable.
	bPSH = 1 << 3
	// bACK assigns the ACK bit in inner peer flag setup; its wire value must remain stable.
	bACK = 1 << 4
	// bURG assigns the URG bit in inner peer flag setup; its wire value must remain stable.
	bURG = 1 << 5
	// bECE assigns the ECE bit in inner peer flag setup; its wire value must remain stable.
	bECE = 1 << 6
	// bCWR assigns the CWR bit in inner peer flag setup; its wire value must remain stable.
	bCWR = 1 << 7
	// bNS assigns the NS bit in inner peer flag setup; its wire value must remain stable.
	bNS = 1 << 8
)

// Proto holds one validated inner ping/flag/target request; application bytes follow on the
// same mux stream.
type Proto struct {
	// RequestID identifies one uncommitted target dial; never reused by retries.
	RequestID [16]byte
	// Validated inner control kind used by engine dispatch.
	Type PType
	// Target hostname/port to be interpreted only for target-opening request kinds.
	Addr *tnet.Addr
	// Requested peer outer flag cycle, carried inside the reliable stream.
	TCPF []conf.TCPF
	// Capability is secret control state; never include it in diagnostic logs.
	Capability [32]byte
	// Epoch prevents a delayed older move from reverting the active tuple.
	Epoch uint64
	// Status is zero for success and one for an unavailable/invalid migration.
	Status byte
}

// encodeTCPF packs configured TCP flag bits into the bounded inner setup message.
func encodeTCPF(f conf.TCPF) uint16 {
	var v uint16
	if f.FIN {
		v |= bFIN
	}
	if f.SYN {
		v |= bSYN
	}
	if f.RST {
		v |= bRST
	}
	if f.PSH {
		v |= bPSH
	}
	if f.ACK {
		v |= bACK
	}
	if f.URG {
		v |= bURG
	}
	if f.ECE {
		v |= bECE
	}
	if f.CWR {
		v |= bCWR
	}
	if f.NS {
		v |= bNS
	}
	return v
}

// decodeTCPF restores outer flag choices from the inner setup bit mask.
func decodeTCPF(v uint16) conf.TCPF {
	return conf.TCPF{
		FIN: v&bFIN != 0, SYN: v&bSYN != 0, RST: v&bRST != 0,
		PSH: v&bPSH != 0, ACK: v&bACK != 0, URG: v&bURG != 0,
		ECE: v&bECE != 0, CWR: v&bCWR != 0, NS: v&bNS != 0,
	}
}

// readFull allocates only the requested bounded control region and rejects truncated input.
func readFull(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	return b, nil
}

// Write serializes the bounded control header/body and uses priority when available so opening
// metadata is not hidden behind bulk.
func (p *Proto) Write(w io.Writer) error {
	body := make([]byte, 0, 64)

	switch p.Type {
	case PPING, PPONG:
		// no body

	case PMTOKEN:
		// Empty request; response uses PMREPLY.
	case PMCHECK, PMMOVE, PMREPLY:
		if p.Status > 1 {
			return errors.New("protocol: invalid migration status")
		}
		body = append(body, p.Capability[:]...)
		body = binary.BigEndian.AppendUint64(body, p.Epoch)
		body = append(body, p.Status)

	case PTCP, PUDP, PTCP2, PUDP2, PTCP3, PUDP3:
		if p.Addr == nil {
			return errors.New("protocol: address required")
		}
		host := []byte(p.Addr.Host)
		if len(host) == 0 || len(host) > maxHostLen {
			return fmt.Errorf("protocol: host length %d exceeds max %d", len(host), maxHostLen)
		}
		if p.Addr.Port < 1 || p.Addr.Port > maxPort {
			return fmt.Errorf("protocol: port %d out of range", p.Addr.Port)
		}
		if p.Type == PTCP3 || p.Type == PUDP3 {
			if p.RequestID == [16]byte{} {
				return errors.New("protocol: opening identity required")
			}
			body = append(body, p.RequestID[:]...)
		}
		body = append(body, byte(len(host)))
		body = append(body, host...)
		body = binary.BigEndian.AppendUint16(body, uint16(p.Addr.Port))

	case PTCPF:
		if len(p.TCPF) == 0 || len(p.TCPF) > maxTCPFCount {
			return fmt.Errorf("protocol: tcpf count %d exceeds max %d", len(p.TCPF), maxTCPFCount)
		}
		body = append(body, byte(len(p.TCPF)))
		for _, f := range p.TCPF {
			body = binary.BigEndian.AppendUint16(body, encodeTCPF(f))
		}

	default:
		return errors.New("protocol: unknown message type")
	}

	if len(body) > maxBodyLen {
		return fmt.Errorf("protocol: body length %d exceeds max %d", len(body), maxBodyLen)
	}

	buf := make([]byte, 0, headerLen+len(body))
	buf = append(buf, MAGIC, VERSION, p.Type)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(body)))
	buf = append(buf, body...)

	var n int
	var err error
	if priority, ok := w.(interface{ WritePriority([]byte) (int, error) }); ok {
		n, err = priority.WritePriority(buf)
	} else {
		n, err = w.Write(buf)
	}
	if err == nil && n != len(buf) {
		return io.ErrShortWrite
	}
	return err
}

// Read validates magic, version, declared lengths and message-specific fields before control
// dispatch.
func (p *Proto) Read(r io.Reader) error {
	hdr, err := readFull(r, headerLen)
	if err != nil {
		return err
	}
	if hdr[0] != MAGIC {
		return fmt.Errorf("protocol: bad magic byte 0x%02x (want 0x%02x)", hdr[0], MAGIC)
	}
	if hdr[1] != VERSION {
		return fmt.Errorf("protocol: unsupported version 0x%02x (want 0x%02x)", hdr[1], VERSION)
	}
	p.Type = hdr[2]
	p.Addr, p.TCPF = nil, nil
	p.RequestID = [16]byte{}
	p.Capability, p.Epoch, p.Status = [32]byte{}, 0, 0

	n := int(binary.BigEndian.Uint16(hdr[3:]))
	if n > maxBodyLen {
		return fmt.Errorf("protocol: body length %d exceeds max %d", n, maxBodyLen)
	}
	body, err := readFull(r, n)
	if err != nil {
		return err
	}

	switch p.Type {
	case PPING, PPONG:
		if len(body) != 0 {
			return errors.New("protocol: unexpected ping body")
		}
		return nil

	case PMTOKEN:
		if len(body) != 0 {
			return errors.New("protocol: unexpected migration token body")
		}
		return nil
	case PMCHECK, PMMOVE, PMREPLY:
		if len(body) != 41 || body[40] > 1 {
			return errors.New("protocol: invalid migration body")
		}
		copy(p.Capability[:], body[:32])
		p.Epoch = binary.BigEndian.Uint64(body[32:40])
		p.Status = body[40]
		return nil

	case PTCP, PUDP, PTCP2, PUDP2, PTCP3, PUDP3:
		if p.Type == PTCP3 || p.Type == PUDP3 {
			if len(body) < 19 {
				return errors.New("protocol: truncated opening identity")
			}
			copy(p.RequestID[:], body[:16])
			body = body[16:]
			if p.RequestID == [16]byte{} {
				return errors.New("protocol: opening identity required")
			}
		}
		if len(body) < 3 {
			return errors.New("protocol: truncated address body")
		}
		hl := int(body[0])
		if hl == 0 || hl > maxHostLen || 1+hl+2 != len(body) {
			return fmt.Errorf("protocol: bad host length %d", hl)
		}
		host := string(body[1 : 1+hl])
		port := int(binary.BigEndian.Uint16(body[1+hl:]))
		if port == 0 {
			return errors.New("protocol: target port cannot be zero")
		}
		p.Addr = &tnet.Addr{Host: host, Port: port}
		return nil

	case PTCPF:
		if len(body) < 1 {
			return errors.New("protocol: truncated tcpf body")
		}
		c := int(body[0])
		if c == 0 || c > maxTCPFCount || 1+c*2 != len(body) {
			return fmt.Errorf("protocol: bad tcpf count %d", c)
		}
		p.TCPF = make([]conf.TCPF, c)
		for i := range p.TCPF {
			p.TCPF[i] = decodeTCPF(binary.BigEndian.Uint16(body[1+i*2:]))
		}
		return nil

	default:
		return errors.New("protocol: unknown message type")
	}
}
