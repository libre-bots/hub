package privacy

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
	"github.com/libre-bots/hub/internal/auth"
	"github.com/libre-bots/hub/internal/discord"
	"github.com/libre-bots/hub/internal/reason"
	"github.com/libre-bots/hub/internal/store/sqlc"
)

type Service struct {
	hubv1.UnimplementedPrivacyServiceServer

	q      *sqlc.Queries
	owners *Owners
	now    func() time.Time
}

func NewService(pool *pgxpool.Pool, owners *Owners, now func() time.Time) *Service {
	return &Service{q: sqlc.New(pool), owners: owners, now: now}
}

func counts(in []CategoryCount) []*hubv1.CategoryCount {
	out := make([]*hubv1.CategoryCount, len(in))
	for i, c := range in {
		out[i] = &hubv1.CategoryCount{Category: c.Category, Count: uint64(max(c.Count, 0))}
	}
	return out
}

func (s *Service) CountUserData(ctx context.Context, req *hubv1.CountUserDataRequest) (*hubv1.CountUserDataResponse, error) {
	if err := discord.ValidateID(req.UserId); err != nil {
		return nil, err
	}
	got, err := s.owners.For(auth.MustClient(ctx).Bot).CountUser(ctx, req.UserId)
	if err != nil {
		return nil, reason.Internal("count user data", err)
	}
	return &hubv1.CountUserDataResponse{Categories: counts(got)}, nil
}

type envelope struct {
	Bot        string `json:"bot"`
	UserID     string `json:"user_id"`
	ExportedAt string `json:"exported_at"`
	Data       any    `json:"data"`
}

func (s *Service) ExportUserData(ctx context.Context, req *hubv1.ExportUserDataRequest) (*hubv1.ExportUserDataResponse, error) {
	if err := discord.ValidateID(req.UserId); err != nil {
		return nil, err
	}
	bot := auth.MustClient(ctx).Bot
	data, err := s.owners.For(bot).ExportUser(ctx, req.UserId)
	if err != nil {
		return nil, reason.Internal("export user data", err)
	}
	if data == nil {
		data = map[string]any{}
	}
	out, err := json.Marshal(envelope{
		Bot:        bot,
		UserID:     strconv.FormatUint(req.UserId, 10),
		ExportedAt: s.now().UTC().Format(time.RFC3339),
		Data:       data,
	})
	if err != nil {
		return nil, reason.Internal("encode user export", err)
	}
	return &hubv1.ExportUserDataResponse{Json: string(out)}, nil
}

func (s *Service) DeleteUserData(ctx context.Context, req *hubv1.DeleteUserDataRequest) (*hubv1.DeleteUserDataResponse, error) {
	if err := discord.ValidateID(req.UserId); err != nil {
		return nil, err
	}
	bot := auth.MustClient(ctx).Bot
	deleted, err := s.owners.For(bot).DeleteUser(ctx, req.UserId)
	if err != nil {
		return nil, reason.Internal("delete user data", err)
	}
	if err := s.q.ClearDeletionRequest(ctx, sqlc.ClearDeletionRequestParams{Bot: bot, UserID: int64(req.UserId)}); err != nil {
		return nil, reason.Internal("clear deletion request", err)
	}
	return &hubv1.DeleteUserDataResponse{Deleted: counts(deleted)}, nil
}

func (s *Service) RequestUserDeletion(ctx context.Context, req *hubv1.RequestUserDeletionRequest) (*hubv1.RequestUserDeletionResponse, error) {
	if err := discord.ValidateID(req.UserId); err != nil {
		return nil, err
	}
	err := s.q.RequestDeletion(ctx, sqlc.RequestDeletionParams{
		Bot:    auth.MustClient(ctx).Bot,
		UserID: int64(req.UserId),
		Now:    s.now(),
	})
	if err != nil {
		return nil, reason.Internal("request user deletion", err)
	}
	return &hubv1.RequestUserDeletionResponse{}, nil
}

func (s *Service) ListPendingUserDeletions(ctx context.Context, _ *hubv1.ListPendingUserDeletionsRequest) (*hubv1.ListPendingUserDeletionsResponse, error) {
	rows, err := s.q.ListPendingDeletions(ctx, auth.MustClient(ctx).Bot)
	if err != nil {
		return nil, reason.Internal("list pending deletions", err)
	}
	out := make([]*hubv1.PendingUserDeletion, len(rows))
	for i, r := range rows {
		out[i] = &hubv1.PendingUserDeletion{UserId: uint64(r.UserID), RequestedAt: timestamppb.New(r.RequestedAt)}
	}
	return &hubv1.ListPendingUserDeletionsResponse{Deletions: out}, nil
}

func (s *Service) CountGuildData(ctx context.Context, req *hubv1.CountGuildDataRequest) (*hubv1.CountGuildDataResponse, error) {
	if err := discord.ValidateID(req.GuildId); err != nil {
		return nil, err
	}
	got, err := s.owners.For(auth.MustClient(ctx).Bot).CountGuild(ctx, req.GuildId)
	if err != nil {
		return nil, reason.Internal("count guild data", err)
	}
	return &hubv1.CountGuildDataResponse{Categories: counts(got)}, nil
}

func (s *Service) DeleteGuildData(ctx context.Context, req *hubv1.DeleteGuildDataRequest) (*hubv1.DeleteGuildDataResponse, error) {
	if err := discord.ValidateID(req.GuildId); err != nil {
		return nil, err
	}
	deleted, err := s.owners.For(auth.MustClient(ctx).Bot).DeleteGuild(ctx, req.GuildId)
	if err != nil {
		return nil, reason.Internal("delete guild data", err)
	}
	return &hubv1.DeleteGuildDataResponse{Deleted: counts(deleted)}, nil
}
