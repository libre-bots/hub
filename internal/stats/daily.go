package stats

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/libre-bots/hub/internal/store/sqlc"
)

const totalScope = "all"

// WriteDaily stores the figures of every bot and their total for the UTC day of now, replacing that day's rows.
func WriteDaily(ctx context.Context, pool *pgxpool.Pool, bots []string, now time.Time) error {
	u := now.UTC()
	day := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	err := pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		perBot, total, err := Compute(ctx, tx, bots)
		if err != nil {
			return fmt.Errorf("compute figures: %w", err)
		}
		q := sqlc.New(tx)
		write := func(scope string, f Figures) error {
			return q.UpsertStatsDaily(ctx, sqlc.UpsertStatsDailyParams{
				Day: day, Scope: scope, GuildCount: f.Guilds, MemberCount: f.Members,
			})
		}
		for _, b := range slices.Sorted(slices.Values(bots)) {
			if err := write(b, perBot[b]); err != nil {
				return err
			}
		}
		return write(totalScope, total)
	})
	if err != nil {
		return fmt.Errorf("write daily figures: %w", err)
	}
	return nil
}
