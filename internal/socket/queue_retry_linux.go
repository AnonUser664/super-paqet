//go:build linux

// File queue_retry_linux.go bounds recovery from transient Linux qdisc pressure
// before a reliable carrier must spend a network retransmission timeout.
package socket

import (
	"golang.org/x/sys/unix"
	"time"
)

// waitTXQueue yields briefly for kernel transmit progress. It runs only after
// ENOBUFS; the normal packet path retains no timer or extra pacing delay.
func waitTXQueue() { time.Sleep(50 * time.Microsecond) }

// sendWithQueueRetry retries only a wholly rejected batch. Successful/partial
// sends return immediately, so an accepted datagram is never replayed here.
// Three brief retries bound syscall/latency cost; persistent congestion remains
// counted datagram loss and is handled by KCP. Readiness/deadline errors retain
// the surrounding RawConn netpoll behavior.
func sendWithQueueRetry(send func() (int, unix.Errno), wait func()) (sent int, errno unix.Errno, retries int) {
	for {
		sent, errno = send()
		if errno != unix.ENOBUFS || retries == 3 {
			return
		}
		retries++
		wait()
	}
}
