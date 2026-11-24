package server_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"

	"github.com/libre-bots/hub/internal/auth"
	"github.com/libre-bots/hub/internal/server"
	"github.com/libre-bots/hub/internal/testkit"
)

type testService struct {
	grpc_testing.UnimplementedTestServiceServer
	done chan struct{}
}

func (s *testService) EmptyCall(ctx context.Context, _ *grpc_testing.Empty) (*grpc_testing.Empty, error) {
	if err := grpc.SetHeader(ctx, metadata.Pairs("bot", auth.MustClient(ctx).Bot)); err != nil {
		return nil, err
	}
	return &grpc_testing.Empty{}, nil
}

func (s *testService) StreamingOutputCall(_ *grpc_testing.StreamingOutputCallRequest,
	stream grpc.ServerStreamingServer[grpc_testing.StreamingOutputCallResponse]) error {
	if err := stream.SendHeader(metadata.Pairs("bot", auth.MustClient(stream.Context()).Bot)); err != nil {
		return err
	}
	<-s.done
	return status.Error(codes.Unavailable, "shutting down")
}

func newEnv(t *testing.T, opts ...testkit.Option) (*testkit.Env, *testService) {
	t.Helper()
	svc := &testService{done: make(chan struct{})}
	env := testkit.New(t, append(opts, testkit.WithBot("alpha", &testkit.FakeOwner{}, func(r grpc.ServiceRegistrar) {
		grpc_testing.RegisterTestServiceServer(r, svc)
	}))...)
	go func() {
		<-env.Server.Done()
		close(svc.done)
	}()
	return env, svc
}

func TestHealthNeedsNoToken(t *testing.T) {
	env, _ := newEnv(t)
	resp, err := grpc_health_v1.NewHealthClient(env.Conn(t, "")).Check(t.Context(), &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.Status)
}

func TestCallWithoutTokenIsUnauthenticated(t *testing.T) {
	env, _ := newEnv(t)
	tests := []struct {
		name string
		conn *grpc.ClientConn
	}{
		{"no token", env.Conn(t, "")},
		{"wrong token", env.ConnWithToken(t, "wrong")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := grpc_testing.NewTestServiceClient(tt.conn).EmptyCall(t.Context(), &grpc_testing.Empty{})
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}
}

func TestBotServiceIsClosedToOtherBots(t *testing.T) {
	env, _ := newEnv(t)

	var header metadata.MD
	_, err := grpc_testing.NewTestServiceClient(env.Conn(t, "alpha")).
		EmptyCall(t.Context(), &grpc_testing.Empty{}, grpc.Header(&header))
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha"}, header.Get("bot"))

	_, err = grpc_testing.NewTestServiceClient(env.Conn(t, "beta")).EmptyCall(t.Context(), &grpc_testing.Empty{})
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestReflectionOnlyWhenEnabled(t *testing.T) {
	list := func(env *testkit.Env) error {
		stream, err := grpc_reflection_v1.NewServerReflectionClient(env.Conn(t, "")).ServerReflectionInfo(t.Context())
		require.NoError(t, err)
		err = stream.Send(&grpc_reflection_v1.ServerReflectionRequest{
			MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_ListServices{},
		})
		if !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}
		_, err = stream.Recv()
		return err
	}

	off, _ := newEnv(t)
	assert.Equal(t, codes.Unimplemented, status.Code(list(off)))

	on, _ := newEnv(t, testkit.WithReflection())
	assert.NoError(t, list(on))
}

func TestRegisterBotRequiresOwner(t *testing.T) {
	s := server.New(server.Options{Now: func() time.Time { return time.Time{} }})
	assert.Panics(t, func() { s.RegisterBot("gamma", nil, func(grpc.ServiceRegistrar) {}) })
}

func TestNewRequiresClock(t *testing.T) {
	assert.Panics(t, func() { server.New(server.Options{}) })
}

func TestKeepaliveParams(t *testing.T) {
	sp, ep := server.KeepaliveParams()
	assert.Equal(t, 30*time.Second, sp.Time)
	assert.Equal(t, 10*time.Second, sp.Timeout)
	assert.Equal(t, 10*time.Second, ep.MinTime)
	assert.True(t, ep.PermitWithoutStream)
}

func TestShutdownEndsOpenStreams(t *testing.T) {
	env, _ := newEnv(t)
	stream, err := grpc_testing.NewTestServiceClient(env.Conn(t, "alpha")).
		StreamingOutputCall(t.Context(), &grpc_testing.StreamingOutputCallRequest{})
	require.NoError(t, err)
	header, err := stream.Header()
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha"}, header.Get("bot"))

	recvErr := make(chan error, 1)
	go func() {
		_, err := stream.Recv()
		recvErr <- err
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	returned := make(chan struct{})
	go func() {
		env.Server.Shutdown(ctx)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not return in time")
	}
	assert.Equal(t, codes.Unavailable, status.Code(<-recvErr))
	env.Server.Shutdown(ctx)
}

func TestBotStreamIsClosedToOtherBots(t *testing.T) {
	env, _ := newEnv(t)
	stream, err := grpc_testing.NewTestServiceClient(env.Conn(t, "beta")).
		StreamingOutputCall(t.Context(), &grpc_testing.StreamingOutputCallRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}
