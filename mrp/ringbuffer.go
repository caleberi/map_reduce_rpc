package mrp

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// ringbuffer.go provides a generic lock-free MPMC ring buffer implementation.
//
// Implementation notes:
//   - Capacity is normalized to the next power of two.
//   - `head` and `tail` are monotonic counters (not modulo-limited indexes).
//   - Slot ownership is coordinated by per-slot sequence numbers.

// slot holds a value and a sequence number used to coordinate producers and consumers.
// The sequence protocol:
//
//	seq == index        → slot is empty, ready for a producer to claim
//	seq == index+1      → slot is written, ready for a consumer to claim
//	seq == index+cap    → slot is released, ready for the next lap
type slot[T any] struct {
	sequence atomic.Uint64
	val      T
}

// RingBuffer is a lock-free, multiple-producer/multiple-consumer ring buffer.
// RingBuffer memory layout and head/tail movement:
//
//	index:  0   1   2   3   4   5   6   7  (capacity=8)
//	       [_] [_] [_] [_] [_] [_] [_] [_]
//	        ↑           ↑
//	       head        tail
//
// Producers write at tail, consumers read at head.
// Both wrap around: (index + 1) % capacity
// Buffer is full when (tail + 1) % capacity == head
// Buffer is empty when tail == head
type RingBuffer[T any] struct {
	buffer   unsafe.Pointer // points to []slot[T]
	head     atomic.Uint64
	tail     atomic.Uint64
	capacity uint64
}

// NewRingBuffer creates a lock-free ring buffer with at least `capacity` slots.
//
// The actual capacity is rounded up to the next power of two to allow fast
// index masking (`index & (capacity-1)`).
func NewRingBuffer[T any](capacity uint64) *RingBuffer[T] {
	capacity = nextPowerOfTwo(capacity)

	slots := make([]slot[T], capacity)
	for i := uint64(0); i < capacity; i++ {
		slots[i].sequence.Store(i) // seq == index → empty, ready to write
	}

	rb := &RingBuffer[T]{capacity: capacity}
	atomic.StorePointer(&rb.buffer, unsafe.Pointer(&slots))
	return rb
}

// Cap returns the fixed number of slots in the ring buffer.
func (rb *RingBuffer[T]) Cap() uint64 { return rb.capacity }

// IsFull reports whether the buffer currently contains `Cap()` items.
func (rb *RingBuffer[T]) IsFull() bool { return rb.Len() == rb.capacity }

// IsEmpty reports whether the buffer currently contains no items.
func (rb *RingBuffer[T]) IsEmpty() bool { return rb.Len() == 0 }

// slots atomically loads and returns the current backing slot slice.
func (rb *RingBuffer[T]) slots() []slot[T] {
	return *(*[]slot[T])(atomic.LoadPointer(&rb.buffer))
}

// Push attempts to enqueue `val`.
//
// It returns true on success. If the buffer is full at the moment of the
// attempt, it returns false.
func (rb *RingBuffer[T]) Push(val T) bool {
	slots := rb.slots()
	mask := rb.capacity - 1

	for {
		tail := rb.tail.Load()
		slot := &slots[tail&mask]
		sequence := slot.sequence.Load()
		difference := int64(sequence) - int64(tail)

		switch {
		case difference == 0:
			if rb.tail.CompareAndSwap(tail, tail+1) {
				slot.val = val
				slot.sequence.Store(tail + 1)
				return true
			}
		case difference < 0:
			return false
		default:
			runtime.Gosched()
		}
	}
}

// Pop attempts to dequeue one value.
//
// It returns `(value, true)` on success. If the buffer is empty, it returns
// `(zeroValue, false)`.
func (rb *RingBuffer[T]) Pop() (T, bool) {
	slots := rb.slots()
	mask := rb.capacity - 1

	for {
		head := rb.head.Load()
		slot := &slots[head&mask]
		sequence := slot.sequence.Load()
		difference := int64(sequence) - int64(head+1)

		switch {
		case difference == 0:
			if rb.head.CompareAndSwap(head, head+1) {
				val := slot.val
				slot.sequence.Store(head + rb.capacity)
				return val, true
			}
		case difference < 0:
			var zero T
			return zero, false
		default:
			runtime.Gosched()
		}
	}
}

// Drain repeatedly pops values into `dst` until `dst` is full or the buffer is
// empty. It returns the number of values written to `dst`.
func (rb *RingBuffer[T]) Drain(dst []T) int {
	n := 0
	for n < len(dst) {
		val, ok := rb.Pop()
		if !ok {
			break
		}
		dst[n] = val
		n++
	}
	return n
}

// Len returns an approximate current item count (`tail - head`).
//
// Under concurrent access this value may change immediately after being read.
func (rb *RingBuffer[T]) Len() uint64 {
	tail := rb.tail.Load()
	head := rb.head.Load()
	if tail > head {
		return tail - head
	}
	return 0
}

// nextPowerOfTwo rounds n up to the next power of two.
func nextPowerOfTwo(n uint64) uint64 {
	if n == 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}
