// File iterator.go: advances immutable flag cycles atomically so shared packet writers do not
// race index updates.

package iterator

import "sync/atomic"

// Iterator cycles an immutable item slice using an atomic index; callers must provide a
// nonempty cycle.
type Iterator[T any] struct {
	// Immutable nonempty cycle; mutation after sharing would violate reader assumptions.
	Items []T
	// Atomic position in the immutable cycle.
	index atomic.Uint64
}

// Next advances the immutable flag cycle atomically; a power-of-two length avoids division in
// the packet hot path.
func (it *Iterator[T]) Next() T {
	i := it.index.Add(1) - 1
	n := uint64(len(it.Items))
	if n&(n-1) == 0 {
		return it.Items[i&(n-1)]
	}
	return it.Items[i%n]
}

// Peek reads the current cycle selection without advancing shared writer state.
func (it *Iterator[T]) Peek() T {
	n := len(it.Items)
	i := it.index.Load()
	return it.Items[i%uint64(n)]
}
