package server_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"github.com/libre-bots/hub/internal/reason"
	"github.com/libre-bots/hub/internal/testkit"
)

type failingService struct {
	grpc_testing.UnimplementedTestServiceServer
	err error
}

func (s *failingService) EmptyCall(context.Context, *grpc_testing.Empty) (*grpc_testing.Empty, error) {
	return nil, s.err
}

func (s *failingService) StreamingOutputCall(*grpc_testing.StreamingOutputCallRequest,
	grpc.ServerStreamingServer[grpc_testing.StreamingOutputCallResponse]) error {
	return s.err
}

func logEnv(t *testing.T, err error) (grpc_testing.TestServiceClient, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	svc := &failingService{err: err}
	env := testkit.New(t, testkit.WithLogger(logger),
		testkit.WithBot("alpha", &testkit.FakeOwner{}, func(r grpc.ServiceRegistrar) {
			grpc_testing.RegisterTestServiceServer(r, svc)
		}))
	return grpc_testing.NewTestServiceClient(env.Conn(t, "alpha")), &buf
}

func TestInternalErrorIsLoggedAndHidden(t *testing.T) {
	c, buf := logEnv(t, reason.Internal("load thing", errors.New("connection reset")))

	_, err := c.EmptyCall(t.Context(), &grpc_testing.Empty{})
	require.Equal(t, codes.Internal, status.Code(err))
	require.NotContains(t, status.Convert(err).Message(), "connection reset")

	require.Equal(t, 1, strings.Count(buf.String(), "\n"))
	for _, want := range []string{"level=ERROR", "connection reset", "/grpc.testing.TestService/EmptyCall", "bot=alpha"} {
		require.Contains(t, buf.String(), want)
	}
}

func TestStreamInternalErrorIsLogged(t *testing.T) {
	c, buf := logEnv(t, reason.Internal("load thing", errors.New("connection reset")))

	stream, err := c.StreamingOutputCall(t.Context(), &grpc_testing.StreamingOutputCallRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, buf.String(), "connection reset")
	require.Contains(t, buf.String(), "bot=alpha")
}

func TestUnknownErrorIsLogged(t *testing.T) {
	c, buf := logEnv(t, errors.New("plain failure"))
	_, err := c.EmptyCall(t.Context(), &grpc_testing.Empty{})
	require.Equal(t, codes.Unknown, status.Code(err))
	require.Contains(t, buf.String(), "plain failure")
}

func TestOtherErrorsAreNotLogged(t *testing.T) {
	c, buf := logEnv(t, status.Error(codes.NotFound, "nope"))
	_, err := c.EmptyCall(t.Context(), &grpc_testing.Empty{})
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Empty(t, buf.String())
}

func TestDeadlineExceededIsWarnedAndHidden(t *testing.T) {
	c, buf := logEnv(t, reason.Internal("load thing", context.DeadlineExceeded))

	_, err := c.EmptyCall(t.Context(), &grpc_testing.Empty{})
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	require.Equal(t, "deadline exceeded", status.Convert(err).Message())

	require.Equal(t, 1, strings.Count(buf.String(), "\n"))
	for _, want := range []string{"level=WARN", "/grpc.testing.TestService/EmptyCall", "bot=alpha", "error="} {
		require.Contains(t, buf.String(), want)
	}
}

func TestWrappedDeadlineExceededDoesNotLeakItsWrapping(t *testing.T) {
	c, _ := logEnv(t, reason.Internal("load thing", fmt.Errorf("query guilds: %w", context.DeadlineExceeded)))

	_, err := c.EmptyCall(t.Context(), &grpc_testing.Empty{})
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	require.Equal(t, "deadline exceeded", status.Convert(err).Message())
}

func TestCanceledIsNotLogged(t *testing.T) {
	for name, err := range map[string]error{
		"internal": reason.Internal("load thing", context.Canceled),
		"status":   status.FromContextError(context.Canceled).Err(),
		"wrapped":  fmt.Errorf("load thing: %w", context.Canceled),
	} {
		t.Run(name, func(t *testing.T) {
			c, buf := logEnv(t, err)
			_, got := c.EmptyCall(t.Context(), &grpc_testing.Empty{})
			require.Error(t, got)
			require.Empty(t, buf.String())

			stream, serr := c.StreamingOutputCall(t.Context(), &grpc_testing.StreamingOutputCallRequest{})
			require.NoError(t, serr)
			_, serr = stream.Recv()
			require.Error(t, serr)
			require.Empty(t, buf.String())
		})
	}
}
