package server

import (
	"context"
	"log/slog"
	"maps"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
	"github.com/libre-bots/hub/internal/auth"
	"github.com/libre-bots/hub/internal/guilds"
	"github.com/libre-bots/hub/internal/info"
	"github.com/libre-bots/hub/internal/notify"
	"github.com/libre-bots/hub/internal/privacy"
	"github.com/libre-bots/hub/internal/stats"
)

type Options struct {
	Pool       *pgxpool.Pool
	Bots       map[string]string
	Reflection bool
	TLS        credentials.TransportCredentials // nil: plaintext
	Now        func() time.Time
	Notifier   notify.Notifier // nil: in-process
	Logger     *slog.Logger    // nil: discard
	Version    string          // empty: "dev"
}

type Server struct {
	opts   Options
	grpc   *grpc.Server
	access *auth.Access
	owners *privacy.Owners
	health *health.Server
	done   chan struct{}
	once   sync.Once
}

// KeepaliveParams returns the keepalive settings the server is built with.
func KeepaliveParams() (keepalive.ServerParameters, keepalive.EnforcementPolicy) {
	return keepalive.ServerParameters{
		Time:    30 * time.Second,
		Timeout: 10 * time.Second,
	}, keepalive.EnforcementPolicy{
		MinTime:             10 * time.Second,
		PermitWithoutStream: true,
	}
}

func New(o Options) *Server {
	access := auth.NewAccess()
	tokens := auth.NewTokens(o.Bots)
	if o.Now == nil {
		panic("server: Options.Now is required")
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	kp, ep := KeepaliveParams()
	grpcOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(auth.UnaryInterceptor(tokens, access), recoveryUnary(logger), loggingUnary(logger)),
		grpc.ChainStreamInterceptor(auth.StreamInterceptor(tokens, access), recoveryStream(logger), loggingStream(logger)),
		grpc.KeepaliveParams(kp),
		grpc.KeepaliveEnforcementPolicy(ep),
	}
	if o.TLS != nil {
		grpcOpts = append(grpcOpts, grpc.Creds(o.TLS))
	}
	s := &Server{
		opts:   o,
		grpc:   grpc.NewServer(grpcOpts...),
		access: access,
		owners: privacy.NewOwners(),
		health: health.NewServer(),
		done:   make(chan struct{}),
	}
	healthpb.RegisterHealthServer(s.grpc, s.health)
	n := o.Notifier
	if n == nil {
		n = notify.NewLocal(256)
	}
	hubv1.RegisterGuildServiceServer(s.grpc, guilds.NewService(o.Pool, n, o.Now, s.Done()))
	hubv1.RegisterPrivacyServiceServer(s.grpc, privacy.NewService(o.Pool, s.owners, o.Now))
	hubv1.RegisterStatsServiceServer(s.grpc, stats.NewService(o.Pool, slices.Sorted(maps.Keys(o.Bots)), o.Now))
	hubv1.RegisterInfoServiceServer(s.grpc, info.NewService(o.Version))
	access.Open(healthpb.Health_ServiceDesc.ServiceName)
	if o.Reflection {
		reflection.Register(s.grpc)
		access.Open("grpc.reflection.v1.ServerReflection", "grpc.reflection.v1alpha.ServerReflection")
	}
	return s
}

// RegisterBot registers a bot's own services together with its DataOwner. Call it before Serve.
func (s *Server) RegisterBot(name string, owner privacy.DataOwner, register func(grpc.ServiceRegistrar)) {
	s.owners.Set(name, owner)
	register(&botRegistrar{server: s.grpc, access: s.access, bot: name})
}

func (s *Server) Owners() *privacy.Owners { return s.owners }

// Serve marks the server healthy and blocks until it stops.
func (s *Server) Serve(lis net.Listener) error {
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	return s.grpc.Serve(lis)
}

// Done is closed when Shutdown begins.
func (s *Server) Done() <-chan struct{} { return s.done }

// Shutdown stops gracefully; ctx bounds only the graceful phase, and it returns once every handler has returned.
func (s *Server) Shutdown(ctx context.Context) {
	s.once.Do(func() { close(s.done) })
	s.health.Shutdown()
	stopped := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-ctx.Done():
		s.grpc.Stop()
		<-stopped
	}
}

type botRegistrar struct {
	server *grpc.Server
	access *auth.Access
	bot    string
}

func (r *botRegistrar) RegisterService(desc *grpc.ServiceDesc, impl any) {
	r.access.Own(desc.ServiceName, r.bot)
	r.server.RegisterService(desc, impl)
}
