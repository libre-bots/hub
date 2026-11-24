package server

import (
	"context"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/libre-bots/hub/internal/auth"
)

func logPanic(ctx context.Context, l *slog.Logger, method string, value any) error {
	attrs := []any{"method", method}
	if c, ok := auth.ClientFrom(ctx); ok {
		attrs = append(attrs, "bot", c.Bot)
	}
	if v, ok := shardsFrom(ctx); ok {
		attrs = append(attrs, "shards", v)
	}
	attrs = append(attrs, "panic", value, "stack", string(debug.Stack()))
	l.ErrorContext(ctx, "handler panicked", attrs...)
	return status.Error(codes.Internal, "internal error")
}

func recoveryUnary(l *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if v := recover(); v != nil {
				resp, err = nil, logPanic(ctx, l, info.FullMethod, v)
			}
		}()
		return handler(ctx, req)
	}
}

func recoveryStream(l *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if v := recover(); v != nil {
				err = logPanic(ss.Context(), l, info.FullMethod, v)
			}
		}()
		return handler(srv, ss)
	}
}
