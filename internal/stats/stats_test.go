package stats_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	hubv1 "github.com/libre-bots/hub/gen/libre/hub/v1"
	"github.com/libre-bots/hub/internal/stats"
	"github.com/libre-bots/hub/internal/testkit"
)

func gid(n uint64) uint64 { return n << 22 }

var bots = []string{"alpha", "beta"}

func join(t *testing.T, env *testkit.Env, bot string, guild uint64, members uint32) {
	t.Helper()
	c := hubv1.NewGuildServiceClient(env.Conn(t, bot))
	_, err := c.GuildJoined(context.Background(), &hubv1.GuildJoinedRequest{GuildId: guild, MemberCount: members})
	require.NoError(t, err)
}

func leave(t *testing.T, env *testkit.Env, bot string, guild uint64) {
	t.Helper()
	c := hubv1.NewGuildServiceClient(env.Conn(t, bot))
	_, err := c.GuildLeft(context.Background(), &hubv1.GuildLeftRequest{GuildId: guild})
	require.NoError(t, err)
}

func TestFiguresPerBotAndTotal(t *testing.T) {
	env := testkit.New(t)
	join(t, env, "alpha", gid(1), 100)
	join(t, env, "alpha", gid(2), 50)
	env.Clock.Advance(time.Minute)
	join(t, env, "beta", gid(1), 120)
	join(t, env, "beta", gid(3), 10)
	join(t, env, "beta", gid(4), 999)
	leave(t, env, "beta", gid(4))

	perBot, total, err := stats.Compute(context.Background(), env.Pool, bots)
	require.NoError(t, err)
	require.Equal(t, map[string]stats.Figures{
		"alpha": {Guilds: 2, Members: 150},
		"beta":  {Guilds: 2, Members: 130},
	}, perBot)
	require.Equal(t, stats.Figures{Guilds: 3, Members: 180}, total)
}

func TestBotWithoutGuildsIsListedWithZeros(t *testing.T) {
	env := testkit.New(t)

	resp, err := hubv1.NewStatsServiceClient(env.Conn(t, "alpha")).GetStats(context.Background(), &hubv1.GetStatsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Bots, 2)
	require.Equal(t, "alpha", resp.Bots[0].Bot)
	require.Equal(t, "beta", resp.Bots[1].Bot)
	for _, b := range resp.Bots {
		require.Zero(t, b.Figures.GuildCount)
		require.Zero(t, b.Figures.MemberCount)
	}
	require.Zero(t, resp.Total.GuildCount)
	require.Zero(t, resp.Total.MemberCount)
}

func TestComputedAtIsNow(t *testing.T) {
	env := testkit.New(t)
	env.Clock.Advance(90 * time.Minute)

	resp, err := hubv1.NewStatsServiceClient(env.Conn(t, "beta")).GetStats(context.Background(), &hubv1.GetStatsRequest{})
	require.NoError(t, err)
	require.Equal(t, env.Clock.Now(), resp.ComputedAt.AsTime())
}

func TestLatestCountWinsTies(t *testing.T) {
	env := testkit.New(t)
	join(t, env, "alpha", gid(1), 100)
	join(t, env, "beta", gid(1), 80)

	_, total, err := stats.Compute(context.Background(), env.Pool, bots)
	require.NoError(t, err)
	require.Equal(t, stats.Figures{Guilds: 1, Members: 100}, total)
}

func TestBotsOutsideTheListAreIgnored(t *testing.T) {
	env := testkit.New(t)
	join(t, env, "alpha", gid(1), 100)
	_, err := env.Pool.Exec(context.Background(),
		`INSERT INTO guilds (bot, guild_id, locale, member_count, counted_at, joined_at)
		 VALUES ('gone', $1, 'en', 500, $2, $2), ('gone', $3, 'en', 7, $2, $2)`,
		int64(gid(1)), env.Clock.Now().Add(time.Hour), int64(gid(9)))
	require.NoError(t, err)

	perBot, total, err := stats.Compute(context.Background(), env.Pool, bots)
	require.NoError(t, err)
	require.Equal(t, map[string]stats.Figures{"alpha": {Guilds: 1, Members: 100}, "beta": {}}, perBot)
	require.Equal(t, stats.Figures{Guilds: 1, Members: 100}, total)
}
