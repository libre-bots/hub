package auth

import (
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Access decides which caller may use which service. Fill it before the server starts.
type Access struct {
	owners map[string]string
	open   map[string]bool
}

func NewAccess() *Access {
	return &Access{owners: map[string]string{}, open: map[string]bool{}}
}

// Own marks service as belonging to bot.
func (a *Access) Own(service, bot string) { a.owners[service] = bot }

// Open makes services callable without a token.
func (a *Access) Open(services ...string) {
	for _, s := range services {
		a.open[s] = true
	}
}

// Check returns nil if c may call fullMethod.
func (a *Access) Check(fullMethod string, c Client, authenticated bool) error {
	service := serviceOf(fullMethod)
	if a.open[service] {
		return nil
	}
	if !authenticated {
		return status.Error(codes.Unauthenticated, "missing or unknown token")
	}
	if owner, ok := a.owners[service]; ok && owner != c.Bot {
		return status.Error(codes.PermissionDenied, "service belongs to another bot")
	}
	return nil
}

func serviceOf(fullMethod string) string {
	rest := strings.TrimPrefix(fullMethod, "/")
	service, _, _ := strings.Cut(rest, "/")
	return service
}
