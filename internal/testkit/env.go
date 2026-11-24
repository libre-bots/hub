package testkit

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/libre-bots/hub/internal/auth"
	"github.com/libre-bots/hub/internal/notify"
	"github.com/libre-bots/hub/internal/privacy"
	"github.com/libre-bots/hub/internal/server"
)

// Env is a running hub on a fresh database, served over an in-memory listener.
type Env struct {
	Pool   *pgxpool.Pool
	Server *server.Server
	Clock  *FakeClock

	lis *bufconn.Listener
}

type config struct {
	reflection bool
	notifier   notify.Notifier
	logger     *slog.Logger
	bots       []func(*server.Server)
}

type Option func(*config)

// WithBot registers a bot's service together with its DataOwner.
func WithBot(name string, owner privacy.DataOwner, register func(grpc.ServiceRegistrar)) Option {
	return func(c *config) {
		c.bots = append(c.bots, func(s *server.Server) { s.RegisterBot(name, owner, register) })
	}
}

func WithReflection() Option {
	return func(c *config) { c.reflection = true }
}

func WithNotifier(n notify.Notifier) Option {
	return func(c *config) { c.notifier = n }
}

func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// New serves bots alpha and beta with the tokens "alpha-token" and "beta-token".
func New(t *testing.T, opts ...Option) *Env {
	t.Helper()
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	clock := NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	pool := NewPool(t)
	srv := server.New(server.Options{
		Pool: pool,
		Bots: map[string]string{
			"alpha": auth.HashToken("alpha-token"),
			"beta":  auth.HashToken("beta-token"),
		},
		Reflection: cfg.reflection,
		Now:        clock.Now,
		Notifier:   cfg.notifier,
		Logger:     cfg.logger,
	})
	for _, register := range cfg.bots {
		register(srv)
	}

	lis := bufconn.Listen(1 << 20)
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		<-served
	})
	return &Env{Pool: pool, Server: srv, Clock: clock, lis: lis}
}

// Conn connects as bot; "" connects without a token.
func (e *Env) Conn(t *testing.T, bot string) *grpc.ClientConn {
	t.Helper()
	if bot == "" {
		return e.ConnWithToken(t, "")
	}
	return e.ConnWithToken(t, bot+"-token")
}

// ConnWithToken connects with an arbitrary bearer token; "" sends none.
func (e *Env) ConnWithToken(t *testing.T, token string) *grpc.ClientConn {
	t.Helper()
	opts := []grpc.DialOption{
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return e.lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if token != "" {
		opts = append(opts,
			grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn,
				invoker grpc.UnaryInvoker, callOpts ...grpc.CallOption) error {
				return invoker(withToken(ctx, token), method, req, reply, cc, callOpts...)
			}),
			grpc.WithStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
				method string, streamer grpc.Streamer, callOpts ...grpc.CallOption) (grpc.ClientStream, error) {
				return streamer(withToken(ctx, token), desc, cc, method, callOpts...)
			}),
		)
	}
	conn, err := grpc.NewClient("passthrough:///hub", opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func withToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}
