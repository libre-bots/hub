package testkit

import (
	"sync"
	"time"
)

// FakeClock is a clock that only moves when told to.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func NewClock(start time.Time) *FakeClock { return &FakeClock{now: start} }

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
