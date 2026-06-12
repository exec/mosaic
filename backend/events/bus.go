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

// Unsubscribe removes ch from the bus and closes it, so a subscriber that is
// done (e.g. a fan-out loop whose context was cancelled) doesn't leave a dead
// channel accumulating in b.subs forever. No-op if ch is not subscribed —
// including after Close, which has already closed and dropped every
// subscriber channel.
func (b *Bus[T]) Unsubscribe(ch <-chan T) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, c := range b.subs {
		if c == ch {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			close(c)
			return
		}
	}
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
