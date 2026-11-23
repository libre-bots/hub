package testkit

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/libre-bots/hub/internal/store"
)

const templateName = "hub_template"

var (
	adminURL string
	counter  atomic.Uint64
)

// Main starts one Postgres container for the test binary and runs the tests.
func Main(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres:18-alpine",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("hub"),
		postgres.WithPassword("hub"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Printf("start postgres: %v", err)
		return 1
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			log.Printf("terminate postgres: %v", err)
		}
	}()

	adminURL, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("connection string: %v", err)
		return 1
	}
	if err := prepareTemplate(ctx); err != nil {
		log.Printf("prepare template: %v", err)
		return 1
	}
	return m.Run()
}

func prepareTemplate(ctx context.Context) error {
	if err := execAdmin(ctx, "CREATE DATABASE "+templateName); err != nil {
		return err
	}
	pool, err := store.Open(ctx, urlFor(templateName))
	if err != nil {
		return err
	}
	defer pool.Close()
	return store.Migrate(ctx, pool)
}

func urlFor(database string) string {
	u, err := url.Parse(adminURL)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + database
	return u.String()
}

func execAdmin(ctx context.Context, sql string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect as admin: %w", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("%s: %w", sql, err)
	}
	return nil
}

// DatabaseURL returns the URL of a fresh migrated database, dropped when the test ends.
func DatabaseURL(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("test_%d", counter.Add(1))
	if err := execAdmin(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := execAdmin(context.Background(), fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", name)); err != nil {
			t.Errorf("drop database: %v", err)
		}
	})
	return urlFor(name)
}

// NewPool returns a pool on a fresh migrated database, closed and dropped when the test ends.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := store.Open(context.Background(), DatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
