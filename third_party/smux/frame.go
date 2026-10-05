// MIT License
//
// Copyright (c) 2016-2017 xtaci
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// File frame.go: defines the retained mux frame layout and allocation-free header accessors.

package smux

import (
	"encoding/binary"
	"fmt"
)

const ( // cmds
	// protocol version 1:
	cmdSYN byte = iota // stream open
	cmdFIN             // stream close, a.k.a EOF mark
	cmdPSH             // data push
	cmdNOP             // no operation

	// protocol version 2 extra commands
	// notify bytes consumed by remote peer-end
	cmdUPD
	// Optional HalfClose extension: both directions are closed. Distinct from
	// directional FIN so a blocked peer writer can terminate after an abort.
	cmdRST
)

const (
	// data size of cmdUPD, format:
	// |4B data consumed(ACK)| 4B window size(WINDOW) |
	szCmdUPD = 8
)

const (
	// initial peer window guess, a slow-start
	initialPeerWindow = 262144
)

const (
	// sizeOfVer reserves the fixed mux version byte in the retained frame layout.
	sizeOfVer = 1
	// sizeOfCmd reserves the fixed mux command byte in the retained frame layout.
	sizeOfCmd = 1
	// sizeOfLength reserves the two-byte mux payload-length field.
	sizeOfLength = 2
	// sizeOfSid reserves the four-byte session-local logical stream identifier.
	sizeOfSid = 4
	// headerSize sums fixed mux fields so data budgets account for framing overhead.
	headerSize = sizeOfVer + sizeOfCmd + sizeOfSid + sizeOfLength
)

// Frame defines a packet from or to be multiplexed into a single connection
type Frame struct {
	ver  byte   // version
	cmd  byte   // command
	sid  uint32 // stream id
	data []byte // payload
}

// newFrame creates a new frame with given version, command and stream id
func newFrame(version byte, cmd byte, sid uint32) Frame {
	return Frame{ver: version, cmd: cmd, sid: sid}
}

// rawHeader is a byte array representation of Frame header
type rawHeader [headerSize]byte

// Version reads the mux version byte without allocating a decoded header object.
func (h rawHeader) Version() byte {
	return h[0]
}

// Cmd reads the mux command used to select data, credit or lifecycle handling.
func (h rawHeader) Cmd() byte {
	return h[1]
}

// Length reads the bounded little-endian frame payload length.
func (h rawHeader) Length() uint16 {
	return binary.LittleEndian.Uint16(h[2:])
}

// StreamID reads the logical stream identity used for session-local demultiplexing.
func (h rawHeader) StreamID() uint32 {
	return binary.LittleEndian.Uint32(h[4:])
}

// String formats header diagnostics without changing wire contents.
func (h rawHeader) String() string {
	return fmt.Sprintf("Version:%d Cmd:%d StreamID:%d Length:%d",
		h.Version(), h.Cmd(), h.StreamID(), h.Length())
}

// updHeader is a byte array representation of cmdUPD
type updHeader [szCmdUPD]byte

// Consumed reads the cumulative consumed-byte count used by modular credit arithmetic.
func (h updHeader) Consumed() uint32 {
	return binary.LittleEndian.Uint32(h[:])
}

// Window reads advertised receive capacity independently of consumed bytes.
func (h updHeader) Window() uint32 {
	return binary.LittleEndian.Uint32(h[4:])
}
