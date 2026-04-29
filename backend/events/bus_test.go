package events

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sample struct{ N int }

func TestBus_FanOutsToAllSubscribers(t *testing.T) {
	bus := NewBus[sample](16)
	t.Cleanup(bus.Close)

	a := bus.Subscribe()
	b := bus.Subscribe()

	var wg sync.WaitGroup
	wg.Add(2)

	got := func(ch <-chan sample, dst *[]int) {
		defer wg.Done()
		timeout := time.After(time.Second)
		for {
			select {
			case ev := <-ch:
				*dst = append(*dst, ev.N)
				if len(*dst) == 3 {
					return
				}
			case <-timeout:
				return
			}
		}
	}

	var av, bv []int
	go got(a, &av)
	go got(b, &bv)

	bus.Publish(sample{N: 1})
	bus.Publish(sample{N: 2})
	bus.Publish(sample{N: 3})
	wg.Wait()

	require.Equal(t, []int{1, 2, 3}, av)
	require.Equal(t, []int{1, 2, 3}, bv)
}

func TestBus_DropsWhenSubscriberSlow(t *testing.T) {
	bus := NewBus[sample](2)
	t.Cleanup(bus.Close)

	_ = bus.Subscribe() // never reads

	for i := 0; i < 100; i++ {
		bus.Publish(sample{N: i})
	}
	// no panic, no deadlock — bus drops on full buffer
}
