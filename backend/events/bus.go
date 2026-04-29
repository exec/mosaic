package events

import "sync"

// Bus is a typed in-process pub/sub bus. Subscribers each get their own
// buffered channel of size `buf`; if a subscriber's buffer is full, the
// publish is dropped *for that subscriber* (others are not affected).
type Bus[T any] struct {
	mu   sync.RWMutex
	buf  int
	subs []chan T
	done chan struct{}
}

func NewBus[T any](buf int) *Bus[T] {
	return &Bus[T]{buf: buf, done: make(chan struct{})}
}

func (b *Bus[T]) Subscribe() <-chan T {
	ch := make(chan T, b.buf)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	return ch
}

func (b *Bus[T]) Publish(v T) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- v:
		default: // drop on full
		}
	}
}

func (b *Bus[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.done:
		return
	default:
	}
	close(b.done)
	for _, ch := range b.subs {
		close(ch)
	}
	b.subs = nil
}
