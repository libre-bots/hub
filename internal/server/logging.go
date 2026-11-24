package server

import (
	"context"
	"errors"
	"log/slog"
	"regexp"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/libre-bots/hub/internal/auth"
)

// ShardsMetadataKey names the call metadata whose value only labels log records.
const ShardsMetadataKey = "libre-shards"

const maxShardsLen = 512

var shardsPattern = regexp.MustCompile(`^[0-9]{1,5}(,[0-9]{1,5})*/[0-9]{1,5}$`)

func shardsFrom(ctx context.Context) (string, bool) {
	values := metadata.ValueFromIncomingContext(ctx, ShardsMetadataKey)
	if len(values) == 0 || len(values[0]) > maxShardsLen || !shardsPattern.MatchString(values[0]) {
		return "", false
	}
	return values[0], true
}

func logFailure(ctx context.Context, l *slog.Logger, method string, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	level := slog.LevelError
	switch status.Code(err) {
	case codes.Internal, codes.Unknown:
	case codes.DeadlineExceeded:
		level = slog.LevelWarn
	default:
		return
	}
	attrs := []any{"method", method}
	if c, ok := auth.ClientFrom(ctx); ok {
		attrs = append(attrs, "bot", c.Bot)
	}
	if v, ok := shardsFrom(ctx); ok {
		attrs = append(attrs, "shards", v)
	}
	attrs = append(attrs, "error", err.Error())
	l.Log(ctx, level, "call failed", attrs...)
}

func loggingUnary(l *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		logFailure(ctx, l, info.FullMethod, err)
		return resp, err
	}
}

func loggingStream(l *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		err := handler(srv, ss)
		logFailure(ss.Context(), l, info.FullMethod, err)
		return err
	}
}
