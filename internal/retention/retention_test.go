package retention_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
	"github.com/libre-bots/hub/internal/privacy"
	"github.com/libre-bots/hub/internal/retention"
	"github.com/libre-bots/hub/internal/testkit"
)

func gid(n uint64) uint64 { return n << 22 }

func setup(t *testing.T) (*testkit.Env, *testkit.FakeOwner) {
	t.Helper()
	owner := &testkit.FakeOwner{FailGuild: map[uint64]error{}}
	env := testkit.New(t, testkit.WithBot("alpha", owner, func(grpc.ServiceRegistrar) {}))
	return env, owner
}

func join(t *testing.T, env *testkit.Env, guild uint64) {
	t.Helper()
	_, err := hubv1.NewGuildServiceClient(env.Conn(t, "alpha")).
		GuildJoined(context.Background(), &hubv1.GuildJoinedRequest{GuildId: guild, MemberCount: 10})
	require.NoError(t, err)
}

func leave(t *testing.T, env *testkit.Env, guild uint64) {
	t.Helper()
	_, err := hubv1.NewGuildServiceClient(env.Conn(t, "alpha")).
		GuildLeft(context.Background(), &hubv1.GuildLeftRequest{GuildId: guild})
	require.NoError(t, err)
}

func run(t *testing.T, env *testkit.Env) (int, error) {
	t.Helper()
	return retention.Run(context.Background(), env.Pool, env.Server.Owners(), env.Clock.Now())
}

func rows(t *testing.T, env *testkit.Env) []int64 {
	t.Helper()
	r, err := env.Pool.Query(context.Background(), `SELECT guild_id FROM guilds WHERE bot = 'alpha' ORDER BY guild_id`)
	require.NoError(t, err)
	defer r.Close()
	var ids []int64
	for r.Next() {
		var id int64
		require.NoError(t, r.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, r.Err())
	return ids
}

func TestRunRemovesGuildsGoneForThirtyDays(t *testing.T) {
	env, owner := setup(t)
	join(t, env, gid(1))
	join(t, env, gid(2))
	join(t, env, gid(3))
	leave(t, env, gid(1))
	env.Clock.Advance(time.Hour + time.Second)
	leave(t, env, gid(2))
	env.Clock.Advance(retention.Keep - time.Hour)

	removed, err := run(t, env)
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	require.Equal(t, []uint64{gid(1)}, owner.DeletedGuilds)
	require.Equal(t, []int64{int64(gid(2)), int64(gid(3))}, rows(t, env))
}

func TestRunKeepsGuildGoneForExactlyThirtyDays(t *testing.T) {
	env, owner := setup(t)
	join(t, env, gid(1))
	leave(t, env, gid(1))
	env.Clock.Advance(retention.Keep)

	removed, err := run(t, env)
	require.NoError(t, err)
	require.Zero(t, removed)
	require.Empty(t, owner.DeletedGuilds)
	require.Equal(t, []int64{int64(gid(1))}, rows(t, env))
}

func TestRunContinuesAfterOwnerError(t *testing.T) {
	env, owner := setup(t)
	join(t, env, gid(1))
	join(t, env, gid(2))
	leave(t, env, gid(1))
	leave(t, env, gid(2))
	env.Clock.Advance(retention.Keep + time.Second)
	boom := errors.New("boom")
	owner.FailGuild[gid(1)] = boom

	removed, err := run(t, env)
	require.Equal(t, 1, removed)
	require.ErrorIs(t, err, boom)
	require.ErrorContains(t, err, "delete guild 4194304 of alpha")
	require.Equal(t, []uint64{gid(2)}, owner.DeletedGuilds)
	require.Equal(t, []int64{int64(gid(1))}, rows(t, env))

	delete(owner.FailGuild, gid(1))
	removed, err = run(t, env)
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	require.Empty(t, rows(t, env))
}

func TestRejoinedGuildIsKept(t *testing.T) {
	env, owner := setup(t)
	join(t, env, gid(1))
	leave(t, env, gid(1))
	env.Clock.Advance(40 * 24 * time.Hour)
	join(t, env, gid(1))

	removed, err := run(t, env)
	require.NoError(t, err)
	require.Zero(t, removed)
	require.Empty(t, owner.DeletedGuilds)
	require.Equal(t, []int64{int64(gid(1))}, rows(t, env))
}

type rejoiningOwner struct {
	*testkit.FakeOwner
	rejoin func()
}

func (o *rejoiningOwner) DeleteGuild(ctx context.Context, id uint64) ([]privacy.CategoryCount, error) {
	if id == gid(1) {
		o.rejoin()
	}
	return o.FakeOwner.DeleteGuild(ctx, id)
}

func TestGuildRejoinedDuringRunIsKept(t *testing.T) {
	fake := &testkit.FakeOwner{}
	owner := &rejoiningOwner{FakeOwner: fake}
	env := testkit.New(t, testkit.WithBot("alpha", owner, func(grpc.ServiceRegistrar) {}))
	owner.rejoin = func() { join(t, env, gid(2)) }
	join(t, env, gid(1))
	join(t, env, gid(2))
	leave(t, env, gid(1))
	leave(t, env, gid(2))
	env.Clock.Advance(retention.Keep + time.Second)

	removed, err := run(t, env)
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	require.Equal(t, []uint64{gid(1)}, fake.DeletedGuilds)
	require.Equal(t, []int64{int64(gid(2))}, rows(t, env))
}
