package guilds_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
	"github.com/libre-bots/hub/internal/guilds"
	"github.com/libre-bots/hub/internal/notify"
	"github.com/libre-bots/hub/internal/reason"
	"github.com/libre-bots/hub/internal/store/sqlc"
	"github.com/libre-bots/hub/internal/testkit"
)

func gid(n uint64) uint64 { return n << 22 }

func client(t *testing.T, env *testkit.Env, bot string) hubv1.GuildServiceClient {
	t.Helper()
	return hubv1.NewGuildServiceClient(env.Conn(t, bot))
}

func shards(count uint32, ids ...uint32) *hubv1.ShardSet {
	return &hubv1.ShardSet{ShardIds: ids, ShardCount: count}
}

func requireReason(t *testing.T, err error, code codes.Code, r hubv1.Reason) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, code, status.Code(err))
	require.Equal(t, r, reason.Of(err))
}

type row struct {
	locale      string
	memberCount int32
	countedAt   time.Time
	joinedAt    time.Time
	leftAt      *time.Time
}

func loadRow(t *testing.T, env *testkit.Env, bot string, guild uint64) row {
	t.Helper()
	var r row
	err := env.Pool.QueryRow(context.Background(),
		`SELECT locale, member_count, counted_at, joined_at, left_at FROM guilds WHERE bot = $1 AND guild_id = $2`,
		bot, int64(guild)).Scan(&r.locale, &r.memberCount, &r.countedAt, &r.joinedAt, &r.leftAt)
	require.NoError(t, err)
	r.countedAt, r.joinedAt = r.countedAt.UTC(), r.joinedAt.UTC()
	if r.leftAt != nil {
		left := r.leftAt.UTC()
		r.leftAt = &left
	}
	return r
}

func countRows(t *testing.T, env *testkit.Env) int {
	t.Helper()
	var n int
	require.NoError(t, env.Pool.QueryRow(context.Background(), `SELECT count(*) FROM guilds`).Scan(&n))
	return n
}

func join(t *testing.T, c hubv1.GuildServiceClient, guild uint64, locale string, members uint32) {
	t.Helper()
	_, err := c.GuildJoined(context.Background(), &hubv1.GuildJoinedRequest{
		GuildId: guild, PreferredLocale: locale, MemberCount: members,
	})
	require.NoError(t, err)
}

func locale(t *testing.T, c hubv1.GuildServiceClient, guild uint64) string {
	t.Helper()
	resp, err := c.GetGuildSettings(context.Background(), &hubv1.GetGuildSettingsRequest{GuildId: guild})
	require.NoError(t, err)
	require.Equal(t, guild, resp.Settings.GuildId)
	return resp.Settings.Locale
}

func TestGuildJoinedRegistersGuild(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	resp, err := c.GuildJoined(context.Background(), &hubv1.GuildJoinedRequest{
		GuildId: gid(2), PreferredLocale: "de", MemberCount: 42,
	})
	require.NoError(t, err)
	require.Equal(t, "de", resp.Settings.Locale)
	require.Equal(t, "de", locale(t, c, gid(2)))
	r := loadRow(t, env, "alpha", gid(2))
	require.EqualValues(t, 42, r.memberCount)
	require.Equal(t, env.Clock.Now(), r.joinedAt)
	require.Equal(t, env.Clock.Now(), r.countedAt)
	require.Nil(t, r.leftAt)
}

func TestGuildJoinedDefaultsLocale(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	join(t, c, gid(2), "", 1)
	require.Equal(t, "en-US", locale(t, c, gid(2)))
}

func TestGuildJoinedTwiceKeepsJoinedAt(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	first := env.Clock.Now()
	join(t, c, gid(2), "de", 10)
	env.Clock.Advance(time.Hour)
	join(t, c, gid(2), "fr", 20)

	r := loadRow(t, env, "alpha", gid(2))
	require.Equal(t, first, r.joinedAt)
	require.EqualValues(t, 20, r.memberCount)
	require.Equal(t, first.Add(time.Hour), r.countedAt)
	require.Equal(t, "de", r.locale)
}

func TestRejoinKeepsLocale(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	join(t, c, gid(2), "en-US", 1)
	_, err := c.UpdateGuildSettings(context.Background(), &hubv1.UpdateGuildSettingsRequest{GuildId: gid(2), Locale: "de"})
	require.NoError(t, err)
	_, err = c.GuildLeft(context.Background(), &hubv1.GuildLeftRequest{GuildId: gid(2)})
	require.NoError(t, err)
	env.Clock.Advance(time.Hour)
	join(t, c, gid(2), "fr", 5)

	r := loadRow(t, env, "alpha", gid(2))
	require.Equal(t, "de", r.locale)
	require.Nil(t, r.leftAt)
	require.Equal(t, env.Clock.Now(), r.joinedAt)
}

func TestGuildLeftIsIdempotent(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	join(t, c, gid(2), "de", 1)
	first := env.Clock.Now()
	_, err := c.GuildLeft(context.Background(), &hubv1.GuildLeftRequest{GuildId: gid(2)})
	require.NoError(t, err)
	env.Clock.Advance(time.Hour)
	_, err = c.GuildLeft(context.Background(), &hubv1.GuildLeftRequest{GuildId: gid(2)})
	require.NoError(t, err)

	r := loadRow(t, env, "alpha", gid(2))
	require.NotNil(t, r.leftAt)
	require.Equal(t, first, *r.leftAt)

	_, err = c.GuildLeft(context.Background(), &hubv1.GuildLeftRequest{GuildId: gid(9)})
	require.NoError(t, err)
	require.Equal(t, 1, countRows(t, env))
}

func TestGetAndUpdateOfLeftGuild(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	join(t, c, gid(2), "de", 1)
	_, err := c.GuildLeft(context.Background(), &hubv1.GuildLeftRequest{GuildId: gid(2)})
	require.NoError(t, err)

	_, err = c.GetGuildSettings(context.Background(), &hubv1.GetGuildSettingsRequest{GuildId: gid(2)})
	requireReason(t, err, codes.NotFound, hubv1.Reason_REASON_GUILD_NOT_REGISTERED)
	_, err = c.UpdateGuildSettings(context.Background(), &hubv1.UpdateGuildSettingsRequest{GuildId: gid(2), Locale: "fr"})
	requireReason(t, err, codes.NotFound, hubv1.Reason_REASON_GUILD_NOT_REGISTERED)
	_, err = c.UpdateGuildSettings(context.Background(), &hubv1.UpdateGuildSettingsRequest{GuildId: gid(7), Locale: "fr"})
	requireReason(t, err, codes.NotFound, hubv1.Reason_REASON_GUILD_NOT_REGISTERED)
}

func TestUpdateStoresLocale(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	join(t, c, gid(2), "en-US", 1)
	resp, err := c.UpdateGuildSettings(context.Background(), &hubv1.UpdateGuildSettingsRequest{GuildId: gid(2), Locale: "es-419"})
	require.NoError(t, err)
	require.Equal(t, "es-419", resp.Settings.Locale)
	require.Equal(t, "es-419", locale(t, c, gid(2)))
}

func TestBotsDoNotSeeEachOther(t *testing.T) {
	env := testkit.New(t)
	join(t, client(t, env, "alpha"), gid(2), "de", 1)
	_, err := client(t, env, "beta").GetGuildSettings(context.Background(), &hubv1.GetGuildSettingsRequest{GuildId: gid(2)})
	requireReason(t, err, codes.NotFound, hubv1.Reason_REASON_GUILD_NOT_REGISTERED)
}

func TestSyncMarksMissingGuildsAsLeft(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	for _, g := range []uint64{gid(2), gid(4), gid(3)} {
		join(t, c, g, "en-US", 1)
	}
	resp, err := c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{
		Shards: shards(2, 0),
		Guilds: []*hubv1.SyncedGuild{
			{GuildId: gid(2), MemberCount: 5},
			{GuildId: gid(6), MemberCount: 7, PreferredLocale: "fr"},
		},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, resp.Joined)
	require.EqualValues(t, 1, resp.Left)
	require.Nil(t, loadRow(t, env, "alpha", gid(2)).leftAt)
	require.EqualValues(t, 5, loadRow(t, env, "alpha", gid(2)).memberCount)
	require.NotNil(t, loadRow(t, env, "alpha", gid(4)).leftAt)
	require.Nil(t, loadRow(t, env, "alpha", gid(3)).leftAt)
	require.Equal(t, "fr", loadRow(t, env, "alpha", gid(6)).locale)
}

func TestSyncDoesNotTouchOtherBots(t *testing.T) {
	env := testkit.New(t)
	join(t, client(t, env, "beta"), gid(2), "en-US", 1)
	_, err := client(t, env, "alpha").SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{Shards: shards(1, 0)})
	require.NoError(t, err)
	require.Nil(t, loadRow(t, env, "beta", gid(2)).leftAt)
}

func TestSyncRejectsGuildOnForeignShard(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	_, err := c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{
		Shards: shards(2, 0),
		Guilds: []*hubv1.SyncedGuild{{GuildId: gid(2)}, {GuildId: gid(3)}},
	})
	requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_SHARDS)
	require.Equal(t, 0, countRows(t, env))
}

func TestSyncRejectsInvalidGuildBeforeWriting(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	_, err := c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{
		Shards: shards(1, 0),
		Guilds: []*hubv1.SyncedGuild{
			{GuildId: gid(2)},
			{GuildId: 0},
		},
	})
	requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_ID)
	_, err = c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{
		Shards: shards(1, 0),
		Guilds: []*hubv1.SyncedGuild{
			{GuildId: gid(2)},
			{GuildId: gid(4), PreferredLocale: "english"},
		},
	})
	requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_LOCALE)
	require.Equal(t, 0, countRows(t, env))
}

func TestSyncWithEmptyListThenRestore(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	join(t, c, gid(2), "de", 1)
	join(t, c, gid(3), "en-US", 1)

	resp, err := c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{Shards: shards(2, 0, 1)})
	require.NoError(t, err)
	require.EqualValues(t, 2, resp.Left)
	require.EqualValues(t, 0, resp.Joined)

	resp, err = c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{Shards: shards(2, 0, 1)})
	require.NoError(t, err)
	require.EqualValues(t, 0, resp.Left)

	env.Clock.Advance(time.Hour)
	resp, err = c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{
		Shards: shards(2, 0, 1),
		Guilds: []*hubv1.SyncedGuild{{GuildId: gid(2), PreferredLocale: "fr"}, {GuildId: gid(3)}},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, resp.Joined)
	require.EqualValues(t, 0, resp.Left)
	for _, g := range []uint64{gid(2), gid(3)} {
		r := loadRow(t, env, "alpha", g)
		require.Nil(t, r.leftAt)
		require.Equal(t, env.Clock.Now(), r.joinedAt)
	}
	require.Equal(t, "de", loadRow(t, env, "alpha", gid(2)).locale)
}

func TestInvalidShards(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	bad := map[string]*hubv1.ShardSet{
		"zero count":  shards(0, 0),
		"id at count": shards(2, 2),
		"empty ids":   shards(2),
		"duplicates":  shards(2, 1, 1),
		"missing set": nil,
	}
	for name, set := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{Shards: set})
			requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_SHARDS)

			stream, err := c.WatchGuildSettings(context.Background(), &hubv1.WatchGuildSettingsRequest{Shards: set})
			require.NoError(t, err)
			_, err = stream.Recv()
			requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_SHARDS)
		})
	}
}

func TestInvalidID(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	ctx := context.Background()
	for _, id := range []uint64{0, 1 << 63} {
		_, err := c.GuildJoined(ctx, &hubv1.GuildJoinedRequest{GuildId: id})
		requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_ID)
		_, err = c.GuildLeft(ctx, &hubv1.GuildLeftRequest{GuildId: id})
		requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_ID)
		_, err = c.GetGuildSettings(ctx, &hubv1.GetGuildSettingsRequest{GuildId: id})
		requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_ID)
		_, err = c.UpdateGuildSettings(ctx, &hubv1.UpdateGuildSettingsRequest{GuildId: id, Locale: "de"})
		requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_ID)
		_, err = c.SyncGuilds(ctx, &hubv1.SyncGuildsRequest{
			Shards: shards(1, 0),
			Guilds: []*hubv1.SyncedGuild{{GuildId: id}},
		})
		requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_ID)
	}
}

func TestLocaleValidation(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	ctx := context.Background()
	join(t, c, gid(2), "en-US", 1)
	for _, l := range []string{"de", "en-US", "es-419"} {
		_, err := c.UpdateGuildSettings(ctx, &hubv1.UpdateGuildSettingsRequest{GuildId: gid(2), Locale: l})
		require.NoError(t, err, l)
	}
	for _, l := range []string{"", "EN", "english", "de_DE"} {
		_, err := c.UpdateGuildSettings(ctx, &hubv1.UpdateGuildSettingsRequest{GuildId: gid(2), Locale: l})
		requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_LOCALE)
	}
	_, err := c.GuildJoined(ctx, &hubv1.GuildJoinedRequest{GuildId: gid(4), PreferredLocale: "english"})
	requireReason(t, err, codes.InvalidArgument, hubv1.Reason_REASON_INVALID_LOCALE)
}

func watch(t *testing.T, c hubv1.GuildServiceClient, set *hubv1.ShardSet) (hubv1.GuildService_WatchGuildSettingsClient, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream, err := c.WatchGuildSettings(ctx, &hubv1.WatchGuildSettingsRequest{Shards: set})
	require.NoError(t, err)
	_, err = stream.Header()
	require.NoError(t, err)
	return stream, cancel
}

func recv(stream hubv1.GuildService_WatchGuildSettingsClient) <-chan *hubv1.WatchGuildSettingsResponse {
	ch := make(chan *hubv1.WatchGuildSettingsResponse, 1)
	go func() {
		resp, err := stream.Recv()
		if err == nil {
			ch <- resp
		}
	}()
	return ch
}

func TestWatchDeliversOwnShardsOnly(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	join(t, c, gid(2), "en-US", 1)
	join(t, c, gid(3), "en-US", 1)
	stream, _ := watch(t, c, shards(2, 0))

	_, err := c.UpdateGuildSettings(context.Background(), &hubv1.UpdateGuildSettingsRequest{GuildId: gid(3), Locale: "fr"})
	require.NoError(t, err)
	_, err = c.UpdateGuildSettings(context.Background(), &hubv1.UpdateGuildSettingsRequest{GuildId: gid(2), Locale: "de"})
	require.NoError(t, err)

	select {
	case resp := <-recv(stream):
		require.Equal(t, gid(2), resp.Settings.GuildId)
		require.Equal(t, "de", resp.Settings.Locale)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no message")
	}
}

func TestWatchDoesNotDeliverOtherBots(t *testing.T) {
	env := testkit.New(t)
	stream, _ := watch(t, client(t, env, "alpha"), shards(1, 0))
	beta := client(t, env, "beta")
	join(t, beta, gid(2), "en-US", 1)
	_, err := beta.UpdateGuildSettings(context.Background(), &hubv1.UpdateGuildSettingsRequest{GuildId: gid(2), Locale: "de"})
	require.NoError(t, err)

	msgs := recv(stream)
	select {
	case <-msgs:
		require.FailNow(t, "alpha received an event of beta")
	case <-time.After(200 * time.Millisecond):
	}

	alpha := client(t, env, "alpha")
	join(t, alpha, gid(4), "en-US", 1)
	_, err = alpha.UpdateGuildSettings(context.Background(), &hubv1.UpdateGuildSettingsRequest{GuildId: gid(4), Locale: "fr"})
	require.NoError(t, err)
	select {
	case resp := <-msgs:
		require.Equal(t, gid(4), resp.Settings.GuildId)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no message")
	}
}

func TestWatchSkipsGuildThatLeft(t *testing.T) {
	n := notify.NewLocal(8)
	env := testkit.New(t, testkit.WithNotifier(n))
	c := client(t, env, "alpha")
	join(t, c, gid(2), "en-US", 1)
	join(t, c, gid(4), "en-US", 1)
	stream, _ := watch(t, c, shards(2, 0))

	ctx := context.Background()
	_, err := c.GuildLeft(ctx, &hubv1.GuildLeftRequest{GuildId: gid(2)})
	require.NoError(t, err)
	require.NoError(t, n.Publish(ctx, notify.Event{Topic: guilds.TopicSettings, Bot: "alpha", GuildID: gid(2)}))
	_, err = c.UpdateGuildSettings(ctx, &hubv1.UpdateGuildSettingsRequest{GuildId: gid(4), Locale: "fr"})
	require.NoError(t, err)

	select {
	case resp := <-recv(stream):
		require.Equal(t, gid(4), resp.Settings.GuildId)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no message")
	}
}

type closingNotifier struct{ ch chan notify.Event }

func (n *closingNotifier) Publish(context.Context, notify.Event) error { return nil }

func (n *closingNotifier) Subscribe(context.Context, string, string) <-chan notify.Event { return n.ch }

func TestWatchEndsWhenSubscriptionCloses(t *testing.T) {
	n := &closingNotifier{ch: make(chan notify.Event)}
	env := testkit.New(t, testkit.WithNotifier(n))
	stream, _ := watch(t, client(t, env, "alpha"), shards(1, 0))
	close(n.ch)

	_, err := stream.Recv()
	require.Equal(t, codes.Unavailable, status.Code(err))
}

func TestWatchEndsOnShutdown(t *testing.T) {
	env := testkit.New(t)
	stream, _ := watch(t, client(t, env, "alpha"), shards(1, 0))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go env.Server.Shutdown(ctx)

	_, err := stream.Recv()
	require.Equal(t, codes.Unavailable, status.Code(err))
}

func TestMemberCountIsClamped(t *testing.T) {
	env := testkit.New(t)
	c := client(t, env, "alpha")
	join(t, c, gid(2), "en-US", 1<<32-1)
	require.EqualValues(t, math.MaxInt32, loadRow(t, env, "alpha", gid(2)).memberCount)

	_, err := c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{
		Shards: shards(1, 0),
		Guilds: []*hubv1.SyncedGuild{{GuildId: gid(2), MemberCount: 1 << 31}},
	})
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt32, loadRow(t, env, "alpha", gid(2)).memberCount)
}

func TestSyncAcceptsUnsortedGuilds(t *testing.T) {
	env := testkit.New(t)
	resp, err := client(t, env, "alpha").SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{
		Shards: shards(1, 0),
		Guilds: []*hubv1.SyncedGuild{{GuildId: gid(9)}, {GuildId: gid(3)}, {GuildId: gid(5)}},
	})
	require.NoError(t, err)
	require.EqualValues(t, 3, resp.Joined)
}

func TestMarkMissingWithNilSentListMarksAll(t *testing.T) {
	env := testkit.New(t)
	join(t, client(t, env, "alpha"), gid(2), "en-US", 1)
	n, err := sqlc.New(env.Pool).MarkMissingGuildsLeft(context.Background(), sqlc.MarkMissingGuildsLeftParams{
		Now: env.Clock.Now(), Bot: "alpha", ShardCount: 1, ShardIds: []int64{0},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

type failingPublish struct{ notify.Notifier }

func (failingPublish) Publish(context.Context, notify.Event) error { return errors.New("bus down") }

func TestUpdateWithFailingPublish(t *testing.T) {
	env := testkit.New(t, testkit.WithNotifier(failingPublish{notify.NewLocal(1)}))
	c := client(t, env, "alpha")
	join(t, c, gid(2), "en-US", 1)
	_, err := c.UpdateGuildSettings(context.Background(), &hubv1.UpdateGuildSettingsRequest{GuildId: gid(2), Locale: "de"})
	requireReason(t, err, codes.Internal, hubv1.Reason_REASON_UNSPECIFIED)
	require.Equal(t, "de", loadRow(t, env, "alpha", gid(2)).locale)
}

type recordingNotifier struct {
	notify.Notifier
	ctxs chan context.Context
}

func (n *recordingNotifier) Subscribe(ctx context.Context, topic, bot string) <-chan notify.Event {
	n.ctxs <- ctx
	return n.Notifier.Subscribe(ctx, topic, bot)
}

func TestWatchSubscriptionEndsWhenClientCancels(t *testing.T) {
	n := &recordingNotifier{Notifier: notify.NewLocal(1), ctxs: make(chan context.Context, 1)}
	env := testkit.New(t, testkit.WithNotifier(n))
	_, cancel := watch(t, client(t, env, "alpha"), shards(1, 0))
	subCtx := <-n.ctxs
	require.NoError(t, subCtx.Err())

	cancel()
	select {
	case <-subCtx.Done():
	case <-time.After(time.Second):
		require.FailNow(t, "subscription context not done")
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func TestWatchCancelledByClientIsNotLogged(t *testing.T) {
	var buf lockedBuffer
	n := &recordingNotifier{Notifier: notify.NewLocal(1), ctxs: make(chan context.Context, 1)}
	env := testkit.New(t, testkit.WithNotifier(n), testkit.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
	stream, cancel := watch(t, client(t, env, "alpha"), shards(1, 0))
	subCtx := <-n.ctxs

	cancel()
	_, err := stream.Recv()
	require.Equal(t, codes.Canceled, status.Code(err))
	select {
	case <-subCtx.Done():
	case <-time.After(2 * time.Second):
		require.FailNow(t, "subscription not cancelled")
	}
	require.Never(t, func() bool { return buf.Len() > 0 }, 300*time.Millisecond, 10*time.Millisecond)
}

func holdSyncLock(t *testing.T, env *testkit.Env, bot string) (release func()) {
	t.Helper()
	tx, err := env.Pool.Begin(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	require.NoError(t, sqlc.New(env.Pool).WithTx(tx).LockGuildSync(context.Background(), bot))
	return func() { require.NoError(t, tx.Commit(context.Background())) }
}

func syncAsync(c hubv1.GuildServiceClient) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := c.SyncGuilds(context.Background(), &hubv1.SyncGuildsRequest{
			Shards: shards(1, 0),
			Guilds: []*hubv1.SyncedGuild{{GuildId: gid(1), PreferredLocale: "en-US", MemberCount: 1}},
		})
		done <- err
	}()
	return done
}

func TestSyncGuildsWaitsForTheSyncLockOfTheSameBot(t *testing.T) {
	env := testkit.New(t)
	release := holdSyncLock(t, env, "alpha")
	done := syncAsync(client(t, env, "alpha"))

	select {
	case err := <-done:
		require.FailNow(t, "sync returned while the lock was held", "%v", err)
	case <-time.After(300 * time.Millisecond):
	}

	release()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "sync did not return after the lock was released")
	}
}

func TestSyncGuildsIgnoresTheSyncLockOfAnotherBot(t *testing.T) {
	env := testkit.New(t)
	holdSyncLock(t, env, "alpha")
	done := syncAsync(client(t, env, "beta"))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "sync of another bot was blocked")
	}
}
