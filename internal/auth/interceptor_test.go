package auth_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/libre-bots/hub/internal/auth"
)

func TestOpenServiceRunsWithoutClient(t *testing.T) {
	access := auth.NewAccess()
	access.Open("grpc.health.v1.Health")
	tokens := auth.NewTokens(map[string]string{"alpha": auth.HashToken("alpha-token")})
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer alpha-token"))

	called := false
	handler := func(ctx context.Context, _ any) (any, error) {
		called = true
		assert.Panics(t, func() { auth.MustClient(ctx) })
		return nil, nil
	}
	_, err := auth.UnaryInterceptor(tokens, access)(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}, handler)
	assert.NoError(t, err)
	assert.True(t, called)
}
