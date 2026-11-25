package guilds

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
	"github.com/libre-bots/hub/internal/auth"
	"github.com/libre-bots/hub/internal/discord"
	"github.com/libre-bots/hub/internal/reason"
	"github.com/libre-bots/hub/internal/store/sqlc"
)

func (s *Service) WatchGuildSettings(req *hubv1.WatchGuildSettingsRequest, stream grpc.ServerStreamingServer[hubv1.WatchGuildSettingsResponse]) error {
	if err := discord.ValidateShards(req.Shards); err != nil {
		return err
	}
	ctx := stream.Context()
	bot := auth.MustClient(ctx).Bot
	events := s.n.Subscribe(ctx, TopicSettings, bot)
	if err := stream.SendHeader(nil); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case <-s.done:
			return status.Error(codes.Unavailable, "hub is shutting down")
		case e, ok := <-events:
			if !ok {
				return status.Error(codes.Unavailable, "subscription closed")
			}
			if !onShards(e.GuildID, req.Shards) {
				continue
			}
			row, err := s.q.GetGuildSettings(ctx, sqlc.GetGuildSettingsParams{Bot: bot, GuildID: int64(e.GuildID)})
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return reason.Internal("load guild settings", err)
			}
			if err := stream.Send(&hubv1.WatchGuildSettingsResponse{Settings: settings(row.GuildID, row.Locale)}); err != nil {
				return err
			}
		}
	}
}
