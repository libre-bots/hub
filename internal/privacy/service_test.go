package privacy_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
	"github.com/libre-bots/hub/internal/privacy"
	"github.com/libre-bots/hub/internal/reason"
	"github.com/libre-bots/hub/internal/testkit"
)

func gid(n uint64) uint64 { return n << 22 }

func setup(t *testing.T, owner *testkit.FakeOwner) *testkit.Env {
	t.Helper()
	return testkit.New(t, testkit.WithBot("alpha", owner, func(grpc.ServiceRegistrar) {}))
}

func client(t *testing.T, env *testkit.Env, bot string) hubv1.PrivacyServiceClient {
	t.Helper()
	return hubv1.NewPrivacyServiceClient(env.Conn(t, bot))
}

func pending(t *testing.T, c hubv1.PrivacyServiceClient) []*hubv1.PendingUserDeletion {
	t.Helper()
	resp, err := c.ListPendingUserDeletions(context.Background(), &hubv1.ListPendingUserDeletionsRequest{})
	require.NoError(t, err)
	return resp.Deletions
}

func TestCountUserDataAsksTheCallingBotsOwner(t *testing.T) {
	owner := &testkit.FakeOwner{UserCounts: map[uint64][]privacy.CategoryCount{
		7: {{Category: "starboard.entries", Count: 3}},
	}}
	env := setup(t, owner)
	ctx := context.Background()

	resp, err := client(t, env, "alpha").CountUserData(ctx, &hubv1.CountUserDataRequest{UserId: 7})
	require.NoError(t, err)
	require.Len(t, resp.Categories, 1)
	require.Equal(t, "starboard.entries", resp.Categories[0].Category)
	require.Equal(t, uint64(3), resp.Categories[0].Count)

	resp, err = client(t, env, "beta").CountUserData(ctx, &hubv1.CountUserDataRequest{UserId: 7})
	require.NoError(t, err)
	require.Empty(t, resp.Categories)
}

func TestCountGuildDataKeepsOrder(t *testing.T) {
	owner := &testkit.FakeOwner{GuildCounts: map[uint64][]privacy.CategoryCount{
		gid(2): {{Category: "b", Count: 1}, {Category: "a", Count: 2}},
	}}
	env := setup(t, owner)

	resp, err := client(t, env, "alpha").CountGuildData(context.Background(), &hubv1.CountGuildDataRequest{GuildId: gid(2)})
	require.NoError(t, err)
	require.Len(t, resp.Categories, 2)
	require.Equal(t, "b", resp.Categories[0].Category)
	require.Equal(t, "a", resp.Categories[1].Category)
	require.Equal(t, uint64(2), resp.Categories[1].Count)
}

func TestExportEnvelope(t *testing.T) {
	owner := &testkit.FakeOwner{Exports: map[uint64]any{7: map[string]any{"entries": []any{"x"}}}}
	env := setup(t, owner)
	ctx := context.Background()

	resp, err := client(t, env, "alpha").ExportUserData(ctx, &hubv1.ExportUserDataRequest{UserId: 7})
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.Json), &got))
	require.Equal(t, "alpha", got["bot"])
	require.Equal(t, "7", got["user_id"])
	require.Equal(t, env.Clock.Now().UTC().Format(time.RFC3339), got["exported_at"])
	require.Equal(t, map[string]any{"entries": []any{"x"}}, got["data"])

	resp, err = client(t, env, "beta").ExportUserData(ctx, &hubv1.ExportUserDataRequest{UserId: 7})
	require.NoError(t, err)
	got = nil
	require.NoError(t, json.Unmarshal([]byte(resp.Json), &got))
	require.Equal(t, "beta", got["bot"])
	require.Equal(t, map[string]any{}, got["data"])
}

func TestRequestUserDeletionIsIdempotent(t *testing.T) {
	env := setup(t, &testkit.FakeOwner{})
	c := client(t, env, "alpha")
	first := env.Clock.Now()

	_, err := c.RequestUserDeletion(context.Background(), &hubv1.RequestUserDeletionRequest{UserId: 7})
	require.NoError(t, err)
	env.Clock.Advance(time.Hour)
	_, err = c.RequestUserDeletion(context.Background(), &hubv1.RequestUserDeletionRequest{UserId: 7})
	require.NoError(t, err)

	got := pending(t, c)
	require.Len(t, got, 1)
	require.Equal(t, uint64(7), got[0].UserId)
	require.True(t, first.Equal(got[0].RequestedAt.AsTime()))
}

func TestListPendingIsPerBotAndOldestFirst(t *testing.T) {
	env := setup(t, &testkit.FakeOwner{})
	ctx := context.Background()
	alpha, beta := client(t, env, "alpha"), client(t, env, "beta")

	request := func(c hubv1.PrivacyServiceClient, id uint64) {
		_, err := c.RequestUserDeletion(ctx, &hubv1.RequestUserDeletionRequest{UserId: id})
		require.NoError(t, err)
	}
	request(alpha, 9)
	request(alpha, 5)
	env.Clock.Advance(time.Minute)
	request(alpha, 1)
	request(beta, 2)

	got := pending(t, alpha)
	require.Len(t, got, 3)
	require.Equal(t, []uint64{5, 9, 1}, []uint64{got[0].UserId, got[1].UserId, got[2].UserId})

	got = pending(t, beta)
	require.Len(t, got, 1)
	require.Equal(t, uint64(2), got[0].UserId)
}

func TestDeleteUserDataClearsTheRequest(t *testing.T) {
	owner := &testkit.FakeOwner{UserCounts: map[uint64][]privacy.CategoryCount{
		7: {{Category: "starboard.entries", Count: 3}},
	}}
	env := setup(t, owner)
	c := client(t, env, "alpha")
	ctx := context.Background()

	_, err := c.RequestUserDeletion(ctx, &hubv1.RequestUserDeletionRequest{UserId: 7})
	require.NoError(t, err)
	resp, err := c.DeleteUserData(ctx, &hubv1.DeleteUserDataRequest{UserId: 7})
	require.NoError(t, err)

	require.Equal(t, []uint64{7}, owner.DeletedUsers)
	require.Len(t, resp.Deleted, 1)
	require.Equal(t, uint64(3), resp.Deleted[0].Count)
	require.Empty(t, pending(t, c))
}

func TestDeleteUserDataWithoutRequest(t *testing.T) {
	owner := &testkit.FakeOwner{}
	env := setup(t, owner)

	_, err := client(t, env, "alpha").DeleteUserData(context.Background(), &hubv1.DeleteUserDataRequest{UserId: 7})
	require.NoError(t, err)
	require.Equal(t, []uint64{7}, owner.DeletedUsers)
}

func TestDeleteUserDataKeepsRequestOnOwnerError(t *testing.T) {
	owner := &testkit.FakeOwner{FailUser: map[uint64]error{7: errors.New("disk on fire")}}
	env := setup(t, owner)
	c := client(t, env, "alpha")
	ctx := context.Background()

	_, err := c.RequestUserDeletion(ctx, &hubv1.RequestUserDeletionRequest{UserId: 7})
	require.NoError(t, err)
	_, err = c.DeleteUserData(ctx, &hubv1.DeleteUserDataRequest{UserId: 7})
	require.Equal(t, codes.Internal, status.Code(err))
	require.Equal(t, hubv1.Reason_REASON_UNSPECIFIED, reason.Of(err))
	require.Len(t, pending(t, c), 1)
}

func TestDeleteGuildDataKeepsTheRegistryRow(t *testing.T) {
	owner := &testkit.FakeOwner{GuildCounts: map[uint64][]privacy.CategoryCount{
		gid(2): {{Category: "starboard.entries", Count: 4}},
	}}
	env := setup(t, owner)
	ctx := context.Background()
	guilds := hubv1.NewGuildServiceClient(env.Conn(t, "alpha"))

	_, err := guilds.GuildJoined(ctx, &hubv1.GuildJoinedRequest{GuildId: gid(2)})
	require.NoError(t, err)
	resp, err := client(t, env, "alpha").DeleteGuildData(ctx, &hubv1.DeleteGuildDataRequest{GuildId: gid(2)})
	require.NoError(t, err)

	require.Equal(t, []uint64{gid(2)}, owner.DeletedGuilds)
	require.Len(t, resp.Deleted, 1)
	require.Equal(t, uint64(4), resp.Deleted[0].Count)
	_, err = guilds.GetGuildSettings(ctx, &hubv1.GetGuildSettingsRequest{GuildId: gid(2)})
	require.NoError(t, err)
}

func TestDeleteGuildDataOwnerError(t *testing.T) {
	owner := &testkit.FakeOwner{FailGuild: map[uint64]error{gid(2): errors.New("boom")}}
	env := setup(t, owner)

	_, err := client(t, env, "alpha").DeleteGuildData(context.Background(), &hubv1.DeleteGuildDataRequest{GuildId: gid(2)})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestInvalidID(t *testing.T) {
	env := setup(t, &testkit.FakeOwner{})
	c := client(t, env, "alpha")
	ctx := context.Background()

	calls := map[string]func() error{
		"CountUserData": func() error {
			_, err := c.CountUserData(ctx, &hubv1.CountUserDataRequest{})
			return err
		},
		"ExportUserData": func() error {
			_, err := c.ExportUserData(ctx, &hubv1.ExportUserDataRequest{})
			return err
		},
		"DeleteUserData": func() error {
			_, err := c.DeleteUserData(ctx, &hubv1.DeleteUserDataRequest{})
			return err
		},
		"RequestUserDeletion": func() error {
			_, err := c.RequestUserDeletion(ctx, &hubv1.RequestUserDeletionRequest{})
			return err
		},
		"CountGuildData": func() error {
			_, err := c.CountGuildData(ctx, &hubv1.CountGuildDataRequest{})
			return err
		},
		"DeleteGuildData": func() error {
			_, err := c.DeleteGuildData(ctx, &hubv1.DeleteGuildDataRequest{})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Equal(t, hubv1.Reason_REASON_INVALID_ID, reason.Of(err))
		})
	}
}
