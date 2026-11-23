package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"os"
	"runtime/debug"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/libre-bots/hub/internal/config"
	"github.com/libre-bots/hub/internal/retention"
	"github.com/libre-bots/hub/internal/server"
	"github.com/libre-bots/hub/internal/stats"
	"github.com/libre-bots/hub/internal/store"
)

var (
	listen             = net.Listen
	openAttemptTimeout = 5 * time.Second
	openDeadline       = 30 * time.Second
)

func serve(ctx context.Context, getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.Load(getenv, os.ReadFile)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(stderr, nil))

	var creds credentials.TransportCredentials
	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		creds, err = credentials.NewServerTLSFromFile(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("load tls certificate: %w", err)
		}
	}

	if _, err := pgxpool.ParseConfig(cfg.DatabaseURL); err != nil {
		return errors.New("DATABASE_URL: cannot be parsed")
	}
	pool, err := openWithRetry(ctx, cfg.DatabaseURL)
	if err != nil {
		return cancelledIsClean(ctx, err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return cancelledIsClean(ctx, fmt.Errorf("migrate: %w", err))
	}

	now := func() time.Time { return time.Now().UTC() }
	bots := slices.Sorted(maps.Keys(cfg.Bots))
	srv := server.New(server.Options{
		Pool:       pool,
		Bots:       cfg.Bots,
		Reflection: cfg.Reflection,
		TLS:        creds,
		Now:        now,
		Logger:     logger,
	})

	lis, err := listen("tcp", cfg.ListenAddr)
	if err != nil {
		return err
	}
	logger.Info("listening", "addr", lis.Addr().String(), "version", version, "bots", bots)
	if len(bots) == 0 {
		logger.Warn("no bot is configured, every call will be rejected")
	}

	jobCtx, stopJobs := context.WithCancel(ctx)
	defer stopJobs()
	jobsDone := make(chan struct{})
	go func() {
		defer close(jobsDone)
		runJobs(jobCtx, logger, pool, srv, bots, now)
	}()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()

	var result error
	select {
	case err := <-serveErr:
		if !errors.Is(err, grpc.ErrServerStopped) {
			result = err
		}
	case <-ctx.Done():
	}
	stopJobs()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	<-jobsDone
	return result
}

func cancelledIsClean(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func openWithRetry(ctx context.Context, url string) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(openDeadline)
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, openAttemptTimeout)
		pool, err := store.Open(attemptCtx, url)
		cancel()
		if err == nil {
			return pool, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("open database: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("open database: %w", err)
		case <-time.After(time.Second):
		}
	}
}

func guarded(logger *slog.Logger, job string, f func()) {
	defer func() {
		if v := recover(); v != nil {
			logger.Error("job panicked", "job", job, "panic", v, "stack", string(debug.Stack()))
		}
	}()
	f()
}

func runJobs(
	ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, srv *server.Server,
	bots []string, now func() time.Time,
) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		guarded(logger, "retention", func() {
			removed, err := retention.Run(ctx, pool, srv.Owners(), now())
			if err != nil && ctx.Err() == nil {
				logger.Error("retention job failed", "error", err)
			} else if err == nil && removed > 0 {
				logger.Info("removed guilds", "count", removed)
			}
		})
		guarded(logger, "daily figures", func() {
			if err := stats.WriteDaily(ctx, pool, bots, now()); err != nil && ctx.Err() == nil {
				logger.Error("daily figures job failed", "error", err)
			}
		})
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
