package mrp

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewRingBufferRoundsCapacityToPowerOfTwo(t *testing.T) {
	rb := NewRingBuffer[int](3)
	assert.Equal(t, uint64(4), rb.Cap())

	rbMin := NewRingBuffer[int](0)
	assert.Equal(t, uint64(1), rbMin.Cap())
}

func TestRingBufferPushPopFIFO(t *testing.T) {
	rb := NewRingBuffer[int](4)

	assert.True(t, rb.IsEmpty())
	assert.Equal(t, uint64(0), rb.Len())

	assert.True(t, rb.Push(10))
	assert.True(t, rb.Push(20))
	assert.True(t, rb.Push(30))

	assert.False(t, rb.IsEmpty())
	assert.Equal(t, uint64(3), rb.Len())

	v1, ok := rb.Pop()
	assert.True(t, ok)
	assert.Equal(t, 10, v1)

	v2, ok := rb.Pop()
	assert.True(t, ok)
	assert.Equal(t, 20, v2)

	v3, ok := rb.Pop()
	assert.True(t, ok)
	assert.Equal(t, 30, v3)

	_, ok = rb.Pop()
	assert.False(t, ok)
	assert.True(t, rb.IsEmpty())
}

func TestRingBufferFullThenRejectPush(t *testing.T) {
	rb := NewRingBuffer[int](2)

	assert.True(t, rb.Push(1))
	assert.True(t, rb.Push(2))
	assert.True(t, rb.IsFull())
	assert.Equal(t, uint64(2), rb.Len())

	assert.False(t, rb.Push(3), "push should fail when buffer is full")

	v, ok := rb.Pop()
	assert.True(t, ok)
	assert.Equal(t, 1, v)

	assert.True(t, rb.Push(3), "push should succeed after one pop")
}

func TestRingBufferDrain(t *testing.T) {
	rb := NewRingBuffer[int](8)

	for i := 1; i <= 5; i++ {
		assert.True(t, rb.Push(i))
	}

	dst := make([]int, 3)
	n := rb.Drain(dst)
	assert.Equal(t, 3, n)
	assert.Equal(t, []int{1, 2, 3}, dst)
	assert.Equal(t, uint64(2), rb.Len())

	dst2 := make([]int, 4)
	n = rb.Drain(dst2)
	assert.Equal(t, 2, n)
	assert.Equal(t, []int{4, 5, 0, 0}, dst2)
	assert.True(t, rb.IsEmpty())
}

func TestRingBufferConcurrentProducersConsumers(t *testing.T) {
	const (
		producers        = 4
		consumers        = 4
		itemsPerProducer = 1000
		totalItems       = producers * itemsPerProducer
	)

	rb := NewRingBuffer[int](256)

	var consumed atomic.Int64
	var doneProducing atomic.Bool

	seen := make([]atomic.Int32, totalItems)
	errCh := make(chan error, 1)
	stop := make(chan struct{})

	reportErr := func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}

	var producerWG sync.WaitGroup
	producerWG.Add(producers)
	for p := 0; p < producers; p++ {
		start := p * itemsPerProducer
		go func(start int) {
			defer producerWG.Done()
			for i := 0; i < itemsPerProducer; i++ {
				value := start + i
				for !rb.Push(value) {
					select {
					case <-stop:
						return
					default:
						runtime.Gosched()
					}
				}
			}
		}(start)
	}

	var consumerWG sync.WaitGroup
	consumerWG.Add(consumers)
	for c := 0; c < consumers; c++ {
		go func() {
			defer consumerWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				value, ok := rb.Pop()
				if !ok {
					if doneProducing.Load() && consumed.Load() >= int64(totalItems) {
						return
					}
					runtime.Gosched()
					continue
				}

				if value < 0 || value >= totalItems {
					reportErr(fmt.Errorf("out-of-range value popped: %d", value))
					continue
				}

				if seen[value].Add(1) != 1 {
					reportErr(fmt.Errorf("duplicate value popped: %d", value))
				}

				consumed.Add(1)
			}
		}()
	}

	producerWG.Wait()
	doneProducing.Store(true)

	assert.Eventually(t, func() bool {
		return consumed.Load() == int64(totalItems)
	}, 3*time.Second, 5*time.Millisecond, "all items should be consumed")

	close(stop)
	consumerWG.Wait()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	default:
	}

	for i := 0; i < totalItems; i++ {
		assert.Equalf(t, int32(1), seen[i].Load(), "expected exactly one observation for value %d", i)
	}

	assert.True(t, rb.IsEmpty())
}

func BenchmarkRingBufferPushPop(b *testing.B) {
	for _, capacity := range []uint64{64, 256, 1024, 4096} {
		b.Run(fmt.Sprintf("cap_%d", capacity), func(b *testing.B) {
			rb := NewRingBuffer[int](capacity)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				for !rb.Push(i) {
					runtime.Gosched()
				}

				for {
					_, ok := rb.Pop()
					if ok {
						break
					}
					runtime.Gosched()
				}
			}
		})
	}
}

func BenchmarkRingBufferParallelContention(b *testing.B) {
	for _, capacity := range []uint64{64, 256, 1024, 4096} {
		b.Run(fmt.Sprintf("cap_%d", capacity), func(b *testing.B) {
			rb := NewRingBuffer[int](capacity)

			b.ReportAllocs()
			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				value := 0
				for pb.Next() {
					for !rb.Push(value) {
						runtime.Gosched()
					}

					for {
						_, ok := rb.Pop()
						if ok {
							break
						}
						runtime.Gosched()
					}

					value++
				}
			})
		})
	}
}
