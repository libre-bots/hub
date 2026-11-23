package gen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	hubv1 "github.com/libre-bots/hub/gen/libre/hub/v1"
)

func TestServicesAreGenerated(t *testing.T) {
	require.Equal(t, "libre.hub.v1.GuildService", hubv1.GuildService_ServiceDesc.ServiceName)
	require.Len(t, hubv1.GuildService_ServiceDesc.Methods, 5)
	require.Len(t, hubv1.GuildService_ServiceDesc.Streams, 1)
	require.Len(t, hubv1.PrivacyService_ServiceDesc.Methods, 7)
	require.Len(t, hubv1.StatsService_ServiceDesc.Methods, 1)
	require.Equal(t, "REASON_GUILD_NOT_REGISTERED", hubv1.Reason_REASON_GUILD_NOT_REGISTERED.String())
}
