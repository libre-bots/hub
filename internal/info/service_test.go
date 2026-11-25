package info_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
	"github.com/libre-bots/hub/internal/testkit"
)

func TestGetInfoReportsVersion(t *testing.T) {
	env := testkit.New(t, testkit.WithVersion("1.2.3"))
	c := hubv1.NewInfoServiceClient(env.Conn(t, "alpha"))

	resp, err := c.GetInfo(context.Background(), &hubv1.GetInfoRequest{})
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", resp.Version)
}

func TestGetInfoDefaultsToDev(t *testing.T) {
	env := testkit.New(t)
	c := hubv1.NewInfoServiceClient(env.Conn(t, "alpha"))

	resp, err := c.GetInfo(context.Background(), &hubv1.GetInfoRequest{})
	require.NoError(t, err)
	assert.Equal(t, "dev", resp.Version)
}

func TestGetInfoNeedsToken(t *testing.T) {
	env := testkit.New(t)
	c := hubv1.NewInfoServiceClient(env.Conn(t, ""))

	_, err := c.GetInfo(context.Background(), &hubv1.GetInfoRequest{})
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}
