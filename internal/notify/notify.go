package notify

import (
	"context"
	"sync"
)

// Event carries ids only; the receiver loads the current value itself.
type Event struct {
	Topic   string
	Bot     string
	GuildID uint64
}

type Notifier interface {
	Publish(ctx context.Context, e Event) error
	// The channel is closed when ctx ends or when the subscriber falls behind.
	Subscribe(ctx context.Context, topic, bot string) <-chan Event
}

var _ Notifier = (*Local)(nil)

type subscriber struct {
	topic, bot string
	ch         chan Event
	stop       func() bool
}

// Local delivers events within one process.
type Local struct {
	buffer int

	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

// NewLocal returns a Local whose subscribers each buffer up to buffer events.
func NewLocal(buffer int) *Local {
	return &Local{buffer: buffer, subs: make(map[*subscriber]struct{})}
}

// Publish never blocks and always returns nil. A subscriber with a full buffer is dropped.
func (l *Local) Publish(_ context.Context, e Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for s := range l.subs {
		if s.topic != e.Topic || s.bot != e.Bot {
			continue
		}
		select {
		case s.ch <- e:
		default:
			l.removeLocked(s)
		}
	}
	return nil
}

func (l *Local) Subscribe(ctx context.Context, topic, bot string) <-chan Event {
	s := &subscriber{topic: topic, bot: bot, ch: make(chan Event, l.buffer)}
	l.mu.Lock()
	defer l.mu.Unlock()
	// The callback waits for mu, so s.stop is set before it can run removeLocked.
	s.stop = context.AfterFunc(ctx, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.removeLocked(s)
	})
	l.subs[s] = struct{}{}
	return s.ch
}

func (l *Local) removeLocked(s *subscriber) {
	// Membership guards the single close.
	if _, ok := l.subs[s]; !ok {
		return
	}
	delete(l.subs, s)
	close(s.ch)
	s.stop()
}
