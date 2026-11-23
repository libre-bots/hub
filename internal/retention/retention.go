package retention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/libre-bots/hub/internal/privacy"
	"github.com/libre-bots/hub/internal/store/sqlc"
)

// Keep is how long the data of a guild is kept after its bot left.
const Keep = 30 * 24 * time.Hour

// Run deletes guilds that left more than Keep before now; failures are joined and do not stop the run.
func Run(ctx context.Context, pool *pgxpool.Pool, owners *privacy.Owners, now time.Time) (removed int, err error) {
	cutoff := now.Add(-Keep)
	due, err := sqlc.New(pool).ListDueGuilds(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("list due guilds: %w", err)
	}
	var errs []error
	for _, g := range due {
		gone, err := removeGuild(ctx, pool, owners, cutoff, g)
		if err != nil {
			errs = append(errs, fmt.Errorf("delete guild %d of %s: %w", g.GuildID, g.Bot, err))
			continue
		}
		if gone {
			removed++
		}
	}
	return removed, errors.Join(errs...)
}

func removeGuild(
	ctx context.Context, pool *pgxpool.Pool, owners *privacy.Owners, cutoff time.Time, g sqlc.ListDueGuildsRow,
) (bool, error) {
	gone := false
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		_, err := q.LockDueGuild(ctx, sqlc.LockDueGuildParams{Bot: g.Bot, GuildID: g.GuildID, Cutoff: cutoff})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := owners.For(g.Bot).DeleteGuild(ctx, uint64(g.GuildID)); err != nil {
			return err
		}
		if err := q.DeleteGuildRow(ctx, sqlc.DeleteGuildRowParams{Bot: g.Bot, GuildID: g.GuildID}); err != nil {
			return err
		}
		gone = true
		return nil
	})
	return gone && err == nil, err
}
