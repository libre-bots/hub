package auth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/libre-bots/hub/internal/auth"
)

func TestAccessCheck(t *testing.T) {
	a := auth.NewAccess()
	a.Open("grpc.health.v1.Health")
	a.Own("alpha.v1.Alpha", "alpha")

	tests := []struct {
		name          string
		method        string
		client        auth.Client
		authenticated bool
		want          codes.Code
	}{
		{"open service without token", "/grpc.health.v1.Health/Check", auth.Client{}, false, codes.OK},
		{"bot service without token", "/alpha.v1.Alpha/Do", auth.Client{}, false, codes.Unauthenticated},
		{"common service without token", "/librebots.hub.v1.Guilds/List", auth.Client{}, false, codes.Unauthenticated},
		{"bot service for its owner", "/alpha.v1.Alpha/Do", auth.Client{Bot: "alpha"}, true, codes.OK},
		{"bot service for another bot", "/alpha.v1.Alpha/Do", auth.Client{Bot: "beta"}, true, codes.PermissionDenied},
		{"common service for any bot", "/librebots.hub.v1.Guilds/List", auth.Client{Bot: "beta"}, true, codes.OK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.Check(tt.method, tt.client, tt.authenticated)
			assert.Equal(t, tt.want, status.Code(err))
		})
	}
}
