package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type clientKey struct{}

// WithClient returns ctx carrying c.
func WithClient(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, clientKey{}, c)
}

// ClientFrom returns the caller of an authenticated call.
func ClientFrom(ctx context.Context) (Client, bool) {
	c, ok := ctx.Value(clientKey{}).(Client)
	return c, ok
}

// MustClient returns the caller of an authenticated call and panics elsewhere.
func MustClient(ctx context.Context) Client {
	c, ok := ClientFrom(ctx)
	if !ok {
		panic("auth: no client in context")
	}
	return c
}

func authorize(ctx context.Context, t *Tokens, a *Access, fullMethod string) (context.Context, error) {
	if a.open[serviceOf(fullMethod)] {
		return ctx, nil
	}
	c, authenticated := Client{}, false
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		for _, v := range md.Get("authorization") {
			token, found := strings.CutPrefix(v, "Bearer ")
			if !found {
				continue
			}
			if c, authenticated = t.Lookup(token); authenticated {
				break
			}
		}
	}
	if err := a.Check(fullMethod, c, authenticated); err != nil {
		return ctx, err
	}
	if authenticated {
		ctx = WithClient(ctx, c)
	}
	return ctx, nil
}

func UnaryInterceptor(t *Tokens, a *Access) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, err := authorize(ctx, t, a, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func StreamInterceptor(t *Tokens, a *Access) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := authorize(ss.Context(), t, a, info.FullMethod)
		if err != nil {
			return err
		}
		return handler(srv, &contextStream{ServerStream: ss, ctx: ctx})
	}
}

type contextStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextStream) Context() context.Context { return s.ctx }
