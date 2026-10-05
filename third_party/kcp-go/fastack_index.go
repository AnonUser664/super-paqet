// File fastack_index.go maintains an allocation-free ordered index of send-ring
// gaps. ACK evidence, timestamps, retry thresholds and wire encoding are unchanged.
package kcp

// pendingSegment resolves a sequence through the contiguous send ring instead
// of retaining a pointer that ring resizing could invalidate.
func (kcp *KCP) pendingSegment(sn uint32) (*segment, bool) {
	seg, ok := kcp.snd_buf.At(int(sn - kcp.snd_una))
	return seg, ok && seg.sn == sn
}

// indexPending builds once on first gap evidence. Selective/cumulative ACKs
// and later appends maintain the index thereafter; no per-ACK map is allocated.
func (kcp *KCP) indexPending() {
	kcp.hasPending = false
	for seg := range kcp.snd_buf.ForEach {
		if seg.acked == 0 {
			kcp.linkPending(seg)
		}
	}
	kcp.pendingIndexed = true
}

// linkPending appends the newest outstanding SN, preserving logical FIFO order
// across physical ring growth and uint32 sequence wraparound.
func (kcp *KCP) linkPending(seg *segment) {
	seg.pendingPrev = kcp.pendingTail
	if kcp.hasPending {
		previous, _ := kcp.pendingSegment(kcp.pendingTail)
		previous.pendingNext = seg.sn
	} else {
		kcp.pendingHead = seg.sn
	}
	kcp.pendingTail, kcp.hasPending = seg.sn, true
}

// unlinkPending removes a newly acknowledged segment before its ring entry is
// recycled. Already acknowledged tombstones are absent and cannot unlink twice.
func (kcp *KCP) unlinkPending(seg *segment) {
	if !kcp.pendingIndexed || !kcp.hasPending || seg.acked != 0 {
		return
	}
	if seg.sn == kcp.pendingHead && seg.sn == kcp.pendingTail {
		kcp.hasPending = false
		return
	}
	if seg.sn == kcp.pendingHead {
		kcp.pendingHead = seg.pendingNext
	} else {
		previous, _ := kcp.pendingSegment(seg.pendingPrev)
		previous.pendingNext = seg.pendingNext
	}
	if seg.sn == kcp.pendingTail {
		kcp.pendingTail = seg.pendingPrev
	} else {
		next, _ := kcp.pendingSegment(seg.pendingNext)
		next.pendingPrev = seg.pendingPrev
	}
}
