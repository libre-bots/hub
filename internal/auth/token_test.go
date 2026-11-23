package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/libre-bots/hub/internal/auth"
)

func TestHashToken(t *testing.T) {
	require.Equal(t, "c91cbbedf8c712e8e2b7517ddeca8fe4fde839ebd8339e0b2001363002b37712", auth.HashToken("dev-token"))
}

func TestNewToken(t *testing.T) {
	a, err := auth.NewToken()
	require.NoError(t, err)
	b, _ := auth.NewToken()
	require.Len(t, a, 43)
	require.NotEqual(t, a, b)
}

func TestLookup(t *testing.T) {
	tokens := auth.NewTokens(map[string]string{"alpha": auth.HashToken("alpha-token")})
	client, ok := tokens.Lookup("alpha-token")
	require.True(t, ok)
	require.Equal(t, "alpha", client.Bot)
	_, ok = tokens.Lookup("other")
	require.False(t, ok)
}
