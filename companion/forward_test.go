package companion

import (
	"context"
	"sync"
	"testing"
	"time"
)

// forwardLoop must follow meshcore-go's channel generations: after a
// connection loss the current channel stays open but silent, and only a
// reconnect publishes a fresh one. A forwarder that caches the channel once
// goes deaf after the first reconnect.
func TestForwardLoopFollowsNewGenerationAfterLoss(t *testing.T) {
	ctx := context.Background()
	gen1 := make(chan interface{}, 1)
	gen2 := make(chan interface{}, 1)

	var mu sync.Mutex
	cur := gen1
	connected := true
	channel := func() <-chan interface{} { mu.Lock(); defer mu.Unlock(); return cur }
	isConnected := func() bool { mu.Lock(); defer mu.Unlock(); return connected }

	var mu2 sync.Mutex
	var got []string
	all := make(chan struct{})
	handle := func(msg interface{}) {
		mu2.Lock()
		got = append(got, msg.(string))
		n := len(got)
		mu2.Unlock()
		if n == 2 {
			close(all)
		}
	}
	done := make(chan struct{})
	go func() {
		forwardLoop(ctx, 5*time.Millisecond, channel, isConnected, handle)
		close(done)
	}()

	gen1 <- "before"

	// Connection lost: markDisconnected leaves the channel open but silent.
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	connected = false
	mu.Unlock()
	// keepalive reconnects: fresh generation, connected again.
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	cur = gen2
	connected = true
	mu.Unlock()
	gen2 <- "after"

	select {
	case <-all:
	case <-time.After(2 * time.Second):
		t.Fatalf("forwarder missed the new generation; got %v", got)
	}
	mu2.Lock()
	order := append([]string(nil), got...)
	mu2.Unlock()
	if order[0] != "before" || order[1] != "after" {
		t.Errorf("unexpected messages: %v", order)
	}
}

func TestForwardLoopStopsOnCtx(t *testing.T) {
	gen := make(chan interface{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		forwardLoop(ctx, 5*time.Millisecond, func() <-chan interface{} { return gen },
			func() bool { return true }, func(interface{}) {})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forwardLoop did not stop on ctx cancellation")
	}
}

// Disconnect() closes the last generation and no reconnect follows: the loop
// must idle (not spin) until ctx is cancelled.
func TestForwardLoopStopsOnCtxWithClosedChannel(t *testing.T) {
	gen := make(chan interface{})
	close(gen)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		forwardLoop(ctx, 5*time.Millisecond, func() <-chan interface{} { return gen },
			func() bool { return true }, func(interface{}) {})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forwardLoop did not stop on ctx cancellation")
	}
}
