package server_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"github.com/libre-bots/hub/internal/testkit"
)

type panickingService struct {
	grpc_testing.UnimplementedTestServiceServer
}

func (panickingService) EmptyCall(context.Context, *grpc_testing.Empty) (*grpc_testing.Empty, error) {
	panic("handler exploded")
}

func (panickingService) StreamingOutputCall(*grpc_testing.StreamingOutputCallRequest,
	grpc.ServerStreamingServer[grpc_testing.StreamingOutputCallResponse]) error {
	panic("stream exploded")
}

func (panickingService) UnaryCall(context.Context, *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
	return &grpc_testing.SimpleResponse{}, nil
}

func panicEnv(t *testing.T) (grpc_testing.TestServiceClient, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	env := testkit.New(t, testkit.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))),
		testkit.WithBot("alpha", &testkit.FakeOwner{}, func(r grpc.ServiceRegistrar) {
			grpc_testing.RegisterTestServiceServer(r, panickingService{})
		}))
	return grpc_testing.NewTestServiceClient(env.Conn(t, "alpha")), &buf
}

func requireOnePanicRecord(t *testing.T, buf *bytes.Buffer, method, value string) {
	t.Helper()
	require.Equal(t, 1, bytes.Count(buf.Bytes(), []byte("\n")))
	for _, want := range []string{"level=ERROR", "method=" + method, "bot=alpha", value, "stack=", "recovery_test.go"} {
		require.Contains(t, buf.String(), want)
	}
}

func TestPanicInUnaryHandlerIsRecovered(t *testing.T) {
	c, buf := panicEnv(t)

	_, err := c.EmptyCall(t.Context(), &grpc_testing.Empty{})
	require.Equal(t, codes.Internal, status.Code(err))
	require.Equal(t, "internal error", status.Convert(err).Message())
	requireOnePanicRecord(t, buf, "/grpc.testing.TestService/EmptyCall", "handler exploded")

	_, err = c.UnaryCall(t.Context(), &grpc_testing.SimpleRequest{})
	require.NoError(t, err)
}

func TestPanicInStreamHandlerIsRecovered(t *testing.T) {
	c, buf := panicEnv(t)

	stream, err := c.StreamingOutputCall(t.Context(), &grpc_testing.StreamingOutputCallRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.Equal(t, codes.Internal, status.Code(err))
	require.Equal(t, "internal error", status.Convert(err).Message())
	requireOnePanicRecord(t, buf, "/grpc.testing.TestService/StreamingOutputCall", "stream exploded")

	_, err = c.UnaryCall(t.Context(), &grpc_testing.SimpleRequest{})
	require.NoError(t, err)
}
