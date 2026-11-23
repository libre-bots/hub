package guilds

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"

	hubv1 "github.com/libre-bots/hub/gen/libre/hub/v1"
	"github.com/libre-bots/hub/internal/auth"
	"github.com/libre-bots/hub/internal/discord"
	"github.com/libre-bots/hub/internal/notify"
	"github.com/libre-bots/hub/internal/reason"
	"github.com/libre-bots/hub/internal/store/sqlc"
)

const TopicSettings = "guild_settings"

type Service struct {
	hubv1.UnimplementedGuildServiceServer

	pool *pgxpool.Pool
	q    *sqlc.Queries
	n    notify.Notifier
	now  func() time.Time
	done <-chan struct{}
}

func NewService(pool *pgxpool.Pool, n notify.Notifier, now func() time.Time, done <-chan struct{}) *Service {
	return &Service{pool: pool, q: sqlc.New(pool), n: n, now: now, done: done}
}

func notRegistered() error {
	return reason.Error(codes.NotFound, hubv1.Reason_REASON_GUILD_NOT_REGISTERED, "guild not registered")
}

func settings(id int64, locale string) *hubv1.GuildSettings {
	return &hubv1.GuildSettings{GuildId: uint64(id), Locale: locale}
}

func memberCount(n uint32) int32 { return int32(min(n, math.MaxInt32)) }

func joinLocale(preferred string) (string, error) {
	if preferred == "" {
		return discord.DefaultLocale, nil
	}
	return preferred, discord.ValidateLocale(preferred)
}

func (s *Service) GuildJoined(ctx context.Context, req *hubv1.GuildJoinedRequest) (*hubv1.GuildJoinedResponse, error) {
	if err := discord.ValidateID(req.GuildId); err != nil {
		return nil, err
	}
	locale, err := joinLocale(req.PreferredLocale)
	if err != nil {
		return nil, err
	}
	row, err := s.q.UpsertGuild(ctx, sqlc.UpsertGuildParams{
		Bot:         auth.MustClient(ctx).Bot,
		GuildID:     int64(req.GuildId),
		Locale:      locale,
		MemberCount: memberCount(req.MemberCount),
		Now:         s.now(),
	})
	if err != nil {
		return nil, reason.Internal("register guild", err)
	}
	return &hubv1.GuildJoinedResponse{Settings: settings(row.GuildID, row.Locale)}, nil
}

func (s *Service) GuildLeft(ctx context.Context, req *hubv1.GuildLeftRequest) (*hubv1.GuildLeftResponse, error) {
	if err := discord.ValidateID(req.GuildId); err != nil {
		return nil, err
	}
	err := s.q.MarkGuildLeft(ctx, sqlc.MarkGuildLeftParams{
		Now:     s.now(),
		Bot:     auth.MustClient(ctx).Bot,
		GuildID: int64(req.GuildId),
	})
	if err != nil {
		return nil, reason.Internal("mark guild left", err)
	}
	return &hubv1.GuildLeftResponse{}, nil
}

type syncedGuild struct {
	id     int64
	locale string
	count  int32
}

func (s *Service) SyncGuilds(ctx context.Context, req *hubv1.SyncGuildsRequest) (*hubv1.SyncGuildsResponse, error) {
	if err := discord.ValidateShards(req.Shards); err != nil {
		return nil, err
	}
	guilds := make([]syncedGuild, 0, len(req.Guilds))
	sent := make([]int64, 0, len(req.Guilds))
	for _, g := range req.Guilds {
		if err := discord.ValidateID(g.GuildId); err != nil {
			return nil, err
		}
		if !onShards(g.GuildId, req.Shards) {
			return nil, reason.Error(codes.InvalidArgument, hubv1.Reason_REASON_INVALID_SHARDS, "guild not on the given shards")
		}
		locale, err := joinLocale(g.PreferredLocale)
		if err != nil {
			return nil, err
		}
		guilds = append(guilds, syncedGuild{id: int64(g.GuildId), locale: locale, count: memberCount(g.MemberCount)})
		sent = append(sent, int64(g.GuildId))
	}

	slices.SortFunc(guilds, func(a, b syncedGuild) int { return cmp.Compare(a.id, b.id) })

	bot := auth.MustClient(ctx).Bot
	now := s.now()
	var joined, left uint32
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		joined, left = 0, 0
		for _, g := range guilds {
			row, err := q.UpsertGuild(ctx, sqlc.UpsertGuildParams{
				Bot: bot, GuildID: g.id, Locale: g.locale, MemberCount: g.count, Now: now,
			})
			if err != nil {
				return err
			}
			if row.Joined {
				joined++
			}
		}
		n, err := q.MarkMissingGuildsLeft(ctx, sqlc.MarkMissingGuildsLeftParams{
			Now:        now,
			Bot:        bot,
			ShardCount: int64(req.Shards.ShardCount),
			ShardIds:   shardIDs(req.Shards),
			Sent:       sent,
		})
		left = uint32(n)
		return err
	})
	if err != nil {
		return nil, reason.Internal("sync guilds", err)
	}
	return &hubv1.SyncGuildsResponse{Joined: joined, Left: left}, nil
}

func (s *Service) GetGuildSettings(ctx context.Context, req *hubv1.GetGuildSettingsRequest) (*hubv1.GetGuildSettingsResponse, error) {
	if err := discord.ValidateID(req.GuildId); err != nil {
		return nil, err
	}
	row, err := s.q.GetGuildSettings(ctx, sqlc.GetGuildSettingsParams{
		Bot:     auth.MustClient(ctx).Bot,
		GuildID: int64(req.GuildId),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notRegistered()
	}
	if err != nil {
		return nil, reason.Internal("load guild settings", err)
	}
	return &hubv1.GetGuildSettingsResponse{Settings: settings(row.GuildID, row.Locale)}, nil
}

func (s *Service) UpdateGuildSettings(ctx context.Context, req *hubv1.UpdateGuildSettingsRequest) (*hubv1.UpdateGuildSettingsResponse, error) {
	if err := discord.ValidateID(req.GuildId); err != nil {
		return nil, err
	}
	if err := discord.ValidateLocale(req.Locale); err != nil {
		return nil, err
	}
	bot := auth.MustClient(ctx).Bot
	row, err := s.q.UpdateGuildLocale(ctx, sqlc.UpdateGuildLocaleParams{
		Locale:  req.Locale,
		Bot:     bot,
		GuildID: int64(req.GuildId),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notRegistered()
	}
	if err != nil {
		return nil, reason.Internal("update guild settings", err)
	}
	if err := s.n.Publish(ctx, notify.Event{Topic: TopicSettings, Bot: bot, GuildID: req.GuildId}); err != nil {
		return nil, reason.Internal("publish settings change", err)
	}
	return &hubv1.UpdateGuildSettingsResponse{Settings: settings(row.GuildID, row.Locale)}, nil
}

func onShards(guildID uint64, set *hubv1.ShardSet) bool {
	shard := discord.ShardOf(guildID, set.ShardCount)
	for _, id := range set.ShardIds {
		if id == shard {
			return true
		}
	}
	return false
}

func shardIDs(set *hubv1.ShardSet) []int64 {
	ids := make([]int64, len(set.ShardIds))
	for i, id := range set.ShardIds {
		ids[i] = int64(id)
	}
	return ids
}
