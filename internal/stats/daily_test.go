package stats_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/libre-bots/hub/internal/stats"
	"github.com/libre-bots/hub/internal/testkit"
)

type dailyRow struct {
	Day     time.Time
	Scope   string
	Guilds  int64
	Members int64
}

func dailyRows(t *testing.T, env *testkit.Env) []dailyRow {
	t.Helper()
	r, err := env.Pool.Query(context.Background(),
		`SELECT day, scope, guild_count, member_count FROM stats_daily ORDER BY day, scope`)
	require.NoError(t, err)
	defer r.Close()
	var out []dailyRow
	for r.Next() {
		var d dailyRow
		require.NoError(t, r.Scan(&d.Day, &d.Scope, &d.Guilds, &d.Members))
		out = append(out, d)
	}
	require.NoError(t, r.Err())
	return out
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestWriteDailyStoresBotsAndTotal(t *testing.T) {
	env := testkit.New(t)
	join(t, env, "alpha", gid(1), 100)
	join(t, env, "alpha", gid(2), 50)
	join(t, env, "beta", gid(1), 120)

	require.NoError(t, stats.WriteDaily(context.Background(), env.Pool, bots, env.Clock.Now()))

	d := day(2026, 1, 1)
	require.Equal(t, []dailyRow{
		{d, "all", 2, 170},
		{d, "alpha", 2, 150},
		{d, "beta", 1, 120},
	}, dailyRows(t, env))
}

func TestWriteDailyTwiceReplacesTheDay(t *testing.T) {
	env := testkit.New(t)
	join(t, env, "alpha", gid(1), 100)
	require.NoError(t, stats.WriteDaily(context.Background(), env.Pool, bots, env.Clock.Now()))
	env.Clock.Advance(time.Hour)
	join(t, env, "alpha", gid(2), 50)
	join(t, env, "beta", gid(3), 7)
	require.NoError(t, stats.WriteDaily(context.Background(), env.Pool, bots, env.Clock.Now()))

	d := day(2026, 1, 1)
	require.Equal(t, []dailyRow{
		{d, "all", 3, 157},
		{d, "alpha", 2, 150},
		{d, "beta", 1, 7},
	}, dailyRows(t, env))
}

func TestDayIsUTC(t *testing.T) {
	env := testkit.New(t)
	now := time.Date(2026, 11, 23, 23, 30, 0, 0, time.FixedZone("", -5*3600))

	require.NoError(t, stats.WriteDaily(context.Background(), env.Pool, bots, now))

	rows := dailyRows(t, env)
	require.Len(t, rows, 3)
	for _, r := range rows {
		require.Equal(t, day(2026, 11, 24), r.Day)
	}
}
