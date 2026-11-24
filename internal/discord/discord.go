package discord

import (
	"regexp"

	"google.golang.org/grpc/codes"

	hubv1 "github.com/libre-bots/hub/gen/libre/hub/v1"
	"github.com/libre-bots/hub/internal/reason"
)

const DefaultLocale = "en-US"

var localePattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2}|-[0-9]{3})?$`)

// ShardOf returns the shard of the guild; shardCount must be at least 1.
func ShardOf(guildID uint64, shardCount uint32) uint32 {
	return uint32((guildID >> 22) % uint64(shardCount))
}

func ValidateShards(s *hubv1.ShardSet) error {
	if s.GetShardCount() < 1 || len(s.GetShardIds()) == 0 {
		return reason.Error(codes.InvalidArgument, hubv1.Reason_REASON_INVALID_SHARDS, "invalid shards")
	}
	seen := make(map[uint32]struct{}, len(s.ShardIds))
	for _, id := range s.ShardIds {
		if _, dup := seen[id]; dup || id >= s.ShardCount {
			return reason.Error(codes.InvalidArgument, hubv1.Reason_REASON_INVALID_SHARDS, "invalid shards")
		}
		seen[id] = struct{}{}
	}
	return nil
}

// ValidateID accepts ids that fit a BIGINT and are not 0.
func ValidateID(id uint64) error {
	if id == 0 || id >= 1<<63 {
		return reason.Error(codes.InvalidArgument, hubv1.Reason_REASON_INVALID_ID, "invalid id")
	}
	return nil
}

func ValidateLocale(locale string) error {
	if !localePattern.MatchString(locale) {
		return reason.Error(codes.InvalidArgument, hubv1.Reason_REASON_INVALID_LOCALE, "invalid locale")
	}
	return nil
}
