package testkit

import (
	"context"
	"sync"

	"github.com/libre-bots/hub/internal/privacy"
)

// FakeOwner is a privacy.DataOwner with configurable answers. Set its fields before use.
type FakeOwner struct {
	UserCounts    map[uint64][]privacy.CategoryCount
	GuildCounts   map[uint64][]privacy.CategoryCount
	Exports       map[uint64]any
	FailGuild     map[uint64]error
	FailUser      map[uint64]error
	DeletedUsers  []uint64
	DeletedGuilds []uint64

	mu sync.Mutex
}

func (f *FakeOwner) CountUser(_ context.Context, id uint64) ([]privacy.CategoryCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.UserCounts[id], nil
}

func (f *FakeOwner) ExportUser(_ context.Context, id uint64) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Exports[id], nil
}

func (f *FakeOwner) DeleteUser(_ context.Context, id uint64) ([]privacy.CategoryCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.FailUser[id]; err != nil {
		return nil, err
	}
	f.DeletedUsers = append(f.DeletedUsers, id)
	return f.UserCounts[id], nil
}

func (f *FakeOwner) CountGuild(_ context.Context, id uint64) ([]privacy.CategoryCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.GuildCounts[id], nil
}

func (f *FakeOwner) DeleteGuild(_ context.Context, id uint64) ([]privacy.CategoryCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.FailGuild[id]; err != nil {
		return nil, err
	}
	f.DeletedGuilds = append(f.DeletedGuilds, id)
	return f.GuildCounts[id], nil
}
