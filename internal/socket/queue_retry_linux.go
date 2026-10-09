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

// sendBatchWithQueueRetry reduces only a wholly rejected prefix. A full qdisc
// may regain room for a few datagrams during the bounded wait, even when it
// cannot absorb the original batch. Partial sends still return immediately;
// the caller retains the unsent tail. On persistent pressure, attempted is the
// exact rejected prefix to count as loss, rather than discarding the whole tail.
// Healthy batches retain their original size and syscall count.
func sendBatchWithQueueRetry(batch int, send func(int) (int, unix.Errno), wait func()) (sent int, errno unix.Errno, retries, attempted int) {
	attempted = batch
	sent, errno, retries = sendWithQueueRetry(func() (int, unix.Errno) {
		n, failure := send(attempted)
		return n, failure
	}, func() {
		attempted = max(1, attempted/2)
		wait()
	})
	return
}
