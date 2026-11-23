package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/libre-bots/hub/internal/store"
	"github.com/libre-bots/hub/internal/testkit"
)

func columns(t *testing.T, pool *pgxpool.Pool, table string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT column_name FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = $1 ORDER BY ordinal_position`, table)
	require.NoError(t, err)
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	return names
}

func TestMigrateCreatesTables(t *testing.T) {
	pool := testkit.NewPool(t)
	require.Equal(t,
		[]string{"bot", "guild_id", "locale", "member_count", "counted_at", "joined_at", "left_at"},
		columns(t, pool, "guilds"))
	require.Equal(t, []string{"bot", "user_id", "requested_at"}, columns(t, pool, "deletion_requests"))
	require.Equal(t, []string{"day", "scope", "guild_count", "member_count"}, columns(t, pool, "stats_daily"))
}

func TestMigrateTwiceChangesNothing(t *testing.T) {
	pool := testkit.NewPool(t)
	require.NoError(t, store.Migrate(context.Background(), pool))
}
