package notify

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func receive(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		require.True(t, ok, "channel closed")
		return e
	case <-time.After(time.Second):
		require.FailNow(t, "no event received")
	}
	return Event{}
}

func requireNothing(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case e, ok := <-ch:
		require.FailNow(t, "unexpected receive", "event %v, open %v", e, ok)
	case <-time.After(50 * time.Millisecond):
	}
}

func requireClosed(t *testing.T, ch <-chan Event) {
	t.Helper()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-time.After(time.Second):
			require.FailNow(t, "channel not closed")
		}
	}
}

func TestSubscriberGetsItsTopicAndBot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := NewLocal(8)
	ch := n.Subscribe(ctx, "settings", "alpha")

	for _, e := range []Event{
		{Topic: "other", Bot: "alpha", GuildID: 1},
		{Topic: "settings", Bot: "beta", GuildID: 2},
	} {
		require.NoError(t, n.Publish(ctx, e))
	}
	want := Event{Topic: "settings", Bot: "alpha", GuildID: 3}
	require.NoError(t, n.Publish(ctx, want))

	assert.Equal(t, want, receive(t, ch))
	requireNothing(t, ch)
}

func TestAllSubscribersGetTheEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := NewLocal(1)
	a := n.Subscribe(ctx, "settings", "alpha")
	b := n.Subscribe(ctx, "settings", "alpha")

	want := Event{Topic: "settings", Bot: "alpha", GuildID: 7}
	require.NoError(t, n.Publish(ctx, want))

	assert.Equal(t, want, receive(t, a))
	assert.Equal(t, want, receive(t, b))
}

func TestCancelClosesTheChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := NewLocal(1)
	ch := n.Subscribe(ctx, "settings", "alpha")

	cancel()
	requireClosed(t, ch)
	assert.NoError(t, n.Publish(context.Background(), Event{Topic: "settings", Bot: "alpha", GuildID: 1}))
}

func TestSlowSubscriberIsDropped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := NewLocal(1)
	slow := n.Subscribe(ctx, "settings", "alpha")
	fast := n.Subscribe(ctx, "settings", "alpha")

	for i := uint64(1); i <= 3; i++ {
		done := make(chan error, 1)
		go func() { done <- n.Publish(ctx, Event{Topic: "settings", Bot: "alpha", GuildID: i}) }()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(100 * time.Millisecond):
			require.FailNow(t, "publish blocked")
		}
		assert.Equal(t, i, receive(t, fast).GuildID)
	}

	requireClosed(t, slow)
}

func TestConcurrentUse(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer stop()
	n := NewLocal(2)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := uint64(0); ctx.Err() == nil; i++ {
				subCtx, cancel := context.WithCancel(ctx)
				ch := n.Subscribe(subCtx, "settings", "alpha")
				assert.NoError(t, n.Publish(ctx, Event{Topic: "settings", Bot: "alpha", GuildID: i + 1}))
				select {
				case <-ch:
				default:
				}
				cancel()
			}
		}()
	}
	wg.Wait()
}
