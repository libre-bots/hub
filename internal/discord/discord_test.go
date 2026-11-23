package discord_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	hubv1 "github.com/libre-bots/hub/gen/libre/hub/v1"
	"github.com/libre-bots/hub/internal/discord"
	"github.com/libre-bots/hub/internal/reason"
)

func TestShardOf(t *testing.T) {
	require.EqualValues(t, 0, discord.ShardOf(175928847299117063, 1))
	require.EqualValues(t, 4, discord.ShardOf(175928847299117063, 16))
}

func TestValidateID(t *testing.T) {
	require.NoError(t, discord.ValidateID(1))
	require.NoError(t, discord.ValidateID(1<<63-1))
	for _, id := range []uint64{0, 1 << 63, 1<<64 - 1} {
		err := discord.ValidateID(id)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Equal(t, hubv1.Reason_REASON_INVALID_ID, reason.Of(err))
	}
}

func TestValidateShards(t *testing.T) {
	tests := []struct {
		name string
		set  *hubv1.ShardSet
		ok   bool
	}{
		{"nil", nil, false},
		{"zero count", &hubv1.ShardSet{ShardIds: []uint32{0}, ShardCount: 0}, false},
		{"id equals count", &hubv1.ShardSet{ShardIds: []uint32{2}, ShardCount: 2}, false},
		{"empty ids", &hubv1.ShardSet{ShardCount: 2}, false},
		{"duplicate ids", &hubv1.ShardSet{ShardIds: []uint32{1, 1}, ShardCount: 2}, false},
		{"one of one", &hubv1.ShardSet{ShardIds: []uint32{0}, ShardCount: 1}, true},
		{"all of two", &hubv1.ShardSet{ShardIds: []uint32{1, 0}, ShardCount: 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := discord.ValidateShards(tt.set)
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Equal(t, hubv1.Reason_REASON_INVALID_SHARDS, reason.Of(err))
		})
	}
}

func TestValidateLocale(t *testing.T) {
	for _, l := range []string{"de", "en-US", "es-419", "fil", "zh-CN"} {
		require.NoError(t, discord.ValidateLocale(l), l)
	}
	for _, l := range []string{"", "EN", "english", "de_DE", "de-us", "d", "de-US\n"} {
		err := discord.ValidateLocale(l)
		require.Equal(t, codes.InvalidArgument, status.Code(err), l)
		require.Equal(t, hubv1.Reason_REASON_INVALID_LOCALE, reason.Of(err), l)
	}
}
