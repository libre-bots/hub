package stats

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	hubv1 "github.com/libre-bots/hub/gen/libre/hub/v1"
	"github.com/libre-bots/hub/internal/reason"
	"github.com/libre-bots/hub/internal/store/sqlc"
)

// Figures counts guilds and the members in them.
type Figures struct{ Guilds, Members int64 }

// Compute returns the figures of every bot in bots, and of all of them together with each guild counted once.
func Compute(
	ctx context.Context, db sqlc.DBTX, bots []string,
) (perBot map[string]Figures, total Figures, err error) {
	q := sqlc.New(db)
	rows, err := q.FiguresPerBot(ctx, bots)
	if err != nil {
		return nil, Figures{}, err
	}
	perBot = make(map[string]Figures, len(bots))
	for _, b := range bots {
		perBot[b] = Figures{}
	}
	for _, r := range rows {
		perBot[r.Bot] = Figures{Guilds: r.Guilds, Members: r.Members}
	}
	t, err := q.FiguresTotal(ctx, bots)
	if err != nil {
		return nil, Figures{}, err
	}
	return perBot, Figures{Guilds: t.Guilds, Members: t.Members}, nil
}

type Service struct {
	hubv1.UnimplementedStatsServiceServer

	pool *pgxpool.Pool
	bots []string
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool, bots []string, now func() time.Time) *Service {
	return &Service{pool: pool, bots: slices.Sorted(slices.Values(bots)), now: now}
}

func figures(f Figures) *hubv1.Figures {
	return &hubv1.Figures{GuildCount: uint64(max(f.Guilds, 0)), MemberCount: uint64(max(f.Members, 0))}
}

func (s *Service) GetStats(ctx context.Context, _ *hubv1.GetStatsRequest) (*hubv1.GetStatsResponse, error) {
	var perBot map[string]Figures
	var total Figures
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) (err error) {
			perBot, total, err = Compute(ctx, tx, s.bots)
			return err
		})
	if err != nil {
		return nil, reason.Internal("compute stats", err)
	}
	out := make([]*hubv1.BotFigures, len(s.bots))
	for i, b := range s.bots {
		out[i] = &hubv1.BotFigures{Bot: b, Figures: figures(perBot[b])}
	}
	return &hubv1.GetStatsResponse{Bots: out, Total: figures(total), ComputedAt: timestamppb.New(s.now().UTC())}, nil
}
