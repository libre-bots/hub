package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
	"github.com/libre-bots/hub/internal/auth"
	"github.com/libre-bots/hub/internal/testkit"
)

type failingListener struct {
	net.Listener
}

func (failingListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }

func TestMain(m *testing.M) {
	testkit.Main(m)
}

func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()
	require.NoError(t, lis.Close())
	return addr
}

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

type running struct {
	addr   string
	env    map[string]string
	cancel context.CancelFunc
	done   chan struct{}
	code   int
	stderr *bytes.Buffer
}

func startHub(t *testing.T, extra map[string]string) *running {
	t.Helper()
	r := &running{
		addr: freeAddr(t),
		env: map[string]string{
			"DATABASE_URL":    testkit.DatabaseURL(t),
			"HUB_LISTEN_ADDR": "",
		},
		done:   make(chan struct{}),
		stderr: &bytes.Buffer{},
	}
	r.env["HUB_LISTEN_ADDR"] = r.addr
	for k, v := range extra {
		r.env[k] = v
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		defer close(r.done)
		r.code = run(ctx, []string{"serve"}, envOf(r.env), &bytes.Buffer{}, r.stderr)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(15 * time.Second):
		}
	})
	require.Eventually(t, func() bool {
		return run(context.Background(), []string{"health"}, envOf(r.env), &bytes.Buffer{}, &bytes.Buffer{}) == 0
	}, 10*time.Second, 50*time.Millisecond)
	return r
}

func dial(t *testing.T, addr string, creds credentials.TransportCredentials) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestServeAnswersAuthenticatedCalls(t *testing.T) {
	r := startHub(t, map[string]string{"HUB_BOT_TOKENS": "example=" + auth.HashToken("dev-token")})
	conn := dial(t, r.addr, insecure.NewCredentials())
	client := hubv1.NewStatsServiceClient(conn)

	ctx := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer dev-token")
	resp, err := client.GetStats(ctx, &hubv1.GetStatsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Bots, 1)
	assert.Equal(t, "example", resp.Bots[0].Bot)

	_, err = client.GetStats(t.Context(), &hubv1.GetStatsRequest{})
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	info, err := hubv1.NewInfoServiceClient(conn).GetInfo(ctx, &hubv1.GetInfoRequest{})
	require.NoError(t, err)
	assert.Equal(t, "dev", info.Version)
}

func TestServeStopsWhenContextEnds(t *testing.T) {
	r := startHub(t, map[string]string{"HUB_BOT_TOKENS": "example=" + auth.HashToken("dev-token")})
	client := hubv1.NewGuildServiceClient(dial(t, r.addr, insecure.NewCredentials()))

	ctx := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer dev-token")
	stream, err := client.WatchGuildSettings(ctx, &hubv1.WatchGuildSettingsRequest{
		Shards: &hubv1.ShardSet{ShardIds: []uint32{0}, ShardCount: 1},
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := stream.Header()
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)

	r.cancel()
	select {
	case <-r.done:
		assert.Equal(t, 0, r.code)
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return")
	}
}

func TestServeWritesDailyFiguresAtStart(t *testing.T) {
	r := startHub(t, nil)
	pool, err := pgxpool.New(t.Context(), r.env["DATABASE_URL"])
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.Eventually(t, func() bool {
		var n int
		err := pool.QueryRow(t.Context(), `SELECT count(*) FROM stats_daily WHERE scope = 'all'`).Scan(&n)
		return err == nil && n > 0
	}, 5*time.Second, 50*time.Millisecond)
}

func TestTokenPrintsTokenAndHash(t *testing.T) {
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(t.Context(), []string{"token"}, envOf(nil), &stdout, &stderr))

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	token, ok := strings.CutPrefix(lines[0], "token: ")
	require.True(t, ok)
	hash, ok := strings.CutPrefix(lines[1], "sha256: ")
	require.True(t, ok)
	assert.Equal(t, auth.HashToken(token), hash)
	assert.Empty(t, stderr.String())
}

func TestVersionPrintsVersion(t *testing.T) {
	var stdout bytes.Buffer
	require.Equal(t, 0, run(t.Context(), []string{"version"}, envOf(nil), &stdout, &bytes.Buffer{}))
	assert.Equal(t, version+"\n", stdout.String())
}

func TestHealth(t *testing.T) {
	env := map[string]string{"HUB_LISTEN_ADDR": freeAddr(t)}
	assert.Equal(t, 1, run(t.Context(), []string{"health"}, envOf(env), &bytes.Buffer{}, &bytes.Buffer{}))

	r := startHub(t, nil)
	assert.Equal(t, 0, run(t.Context(), []string{"health"}, envOf(r.env), &bytes.Buffer{}, &bytes.Buffer{}))
}

func TestHealthTreatsWildcardHostAsLocal(t *testing.T) {
	r := startHub(t, nil)
	_, port, err := net.SplitHostPort(r.addr)
	require.NoError(t, err)
	env := map[string]string{"HUB_LISTEN_ADDR": ":" + port}
	assert.Equal(t, 0, run(t.Context(), []string{"health"}, envOf(env), &bytes.Buffer{}, &bytes.Buffer{}))
}

func selfSigned(t *testing.T) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "hub"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return certFile, keyFile, pool
}

func TestServeWithTLS(t *testing.T) {
	certFile, keyFile, pool := selfSigned(t)
	r := startHub(t, map[string]string{"HUB_TLS_CERT_FILE": certFile, "HUB_TLS_KEY_FILE": keyFile})

	conn := dial(t, r.addr, credentials.NewTLS(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}))
	resp, err := grpc_health_v1.NewHealthClient(conn).Check(t.Context(), &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.Status)

	assert.Equal(t, 0, run(t.Context(), []string{"health"}, envOf(r.env), &bytes.Buffer{}, &bytes.Buffer{}))
}

func TestUnknownCommand(t *testing.T) {
	var stderr bytes.Buffer
	assert.Equal(t, 2, run(t.Context(), []string{"nope"}, envOf(nil), &bytes.Buffer{}, &stderr))
	assert.Contains(t, stderr.String(), "usage")
}

func TestMissingDatabaseURL(t *testing.T) {
	var stderr bytes.Buffer
	assert.Equal(t, 1, run(t.Context(), nil, envOf(nil), &bytes.Buffer{}, &stderr))
	assert.True(t, strings.HasPrefix(stderr.String(), "hub: "))
	assert.Contains(t, stderr.String(), "DATABASE_URL")
}

func TestServeFailureExits(t *testing.T) {
	orig := listen
	t.Cleanup(func() { listen = orig })
	listen = func(network, address string) (net.Listener, error) {
		lis, err := net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		return failingListener{lis}, nil
	}

	env := map[string]string{"DATABASE_URL": testkit.DatabaseURL(t), "HUB_LISTEN_ADDR": freeAddr(t)}
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- run(t.Context(), []string{"serve"}, envOf(env), &bytes.Buffer{}, &stderr) }()
	select {
	case code := <-done:
		assert.Equal(t, 1, code)
		assert.Contains(t, stderr.String(), "hub: ")
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after Serve failed")
	}
}

func TestServeStopsQuietly(t *testing.T) {
	r := startHub(t, nil)
	r.cancel()
	<-r.done
	assert.Equal(t, 0, r.code)
	for _, line := range strings.Split(strings.TrimSpace(r.stderr.String()), "\n") {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry), line)
		assert.NotEqual(t, "ERROR", entry["level"], line)
	}
	assert.NotContains(t, r.stderr.String(), r.env["DATABASE_URL"])
	assert.Contains(t, r.stderr.String(), r.addr)
}

func TestUnparsableDatabaseURLFailsAtOnce(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://hub:s3cret-pass@localhost:notaport/hub"}
	var stderr bytes.Buffer
	start := time.Now()
	assert.Equal(t, 1, run(t.Context(), []string{"serve"}, envOf(env), &bytes.Buffer{}, &stderr))
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.True(t, strings.HasPrefix(stderr.String(), "hub: "))
	assert.NotContains(t, stderr.String(), "s3cret-pass")
}

func TestCancelledDuringDatabaseRetryIsClean(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":    "postgres://hub:hub@" + freeAddr(t) + "/hub?sslmode=disable",
		"HUB_LISTEN_ADDR": freeAddr(t),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	var stderr bytes.Buffer
	assert.Equal(t, 0, run(ctx, []string{"serve"}, envOf(env), &bytes.Buffer{}, &stderr))
	assert.Empty(t, stderr.String())
}

func TestMissingTLSFilesFail(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":      testkit.DatabaseURL(t),
		"HUB_TLS_CERT_FILE": filepath.Join(t.TempDir(), "missing.pem"),
		"HUB_TLS_KEY_FILE":  filepath.Join(t.TempDir(), "missing.key"),
	}
	var stderr bytes.Buffer
	assert.Equal(t, 1, run(t.Context(), []string{"serve"}, envOf(env), &bytes.Buffer{}, &stderr))
	assert.True(t, strings.HasPrefix(stderr.String(), "hub: "))
}

func TestAddressInUseFails(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	env := map[string]string{"DATABASE_URL": testkit.DatabaseURL(t), "HUB_LISTEN_ADDR": lis.Addr().String()}
	var stderr bytes.Buffer
	assert.Equal(t, 1, run(t.Context(), []string{"serve"}, envOf(env), &bytes.Buffer{}, &stderr))
	assert.True(t, strings.HasPrefix(stderr.String(), "hub: "))
}

func logEntries(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry), line)
		entries = append(entries, entry)
	}
	return entries
}

func TestServeLogsVersionAndBotsAtStart(t *testing.T) {
	hash := auth.HashToken("dev-token")
	r := startHub(t, map[string]string{"HUB_BOT_TOKENS": "example=" + hash + ",other=" + auth.HashToken("other-token")})
	r.cancel()
	<-r.done

	var found bool
	for _, entry := range logEntries(t, r.stderr) {
		if entry["msg"] != "listening" {
			continue
		}
		found = true
		assert.Equal(t, r.addr, entry["addr"])
		assert.Equal(t, version, entry["version"])
		assert.Equal(t, []any{"example", "other"}, entry["bots"])
	}
	assert.True(t, found)
	assert.NotContains(t, r.stderr.String(), hash)
}

func TestServeWarnsWhenNoBotIsConfigured(t *testing.T) {
	r := startHub(t, nil)
	r.cancel()
	<-r.done

	var warned bool
	for _, entry := range logEntries(t, r.stderr) {
		if entry["level"] == "WARN" {
			warned = true
			assert.Contains(t, entry["msg"], "no bot")
		}
	}
	assert.True(t, warned)
}

func TestServeDoesNotWarnWhenABotIsConfigured(t *testing.T) {
	r := startHub(t, map[string]string{"HUB_BOT_TOKENS": "example=" + auth.HashToken("dev-token")})
	r.cancel()
	<-r.done

	for _, entry := range logEntries(t, r.stderr) {
		assert.NotEqual(t, "WARN", entry["level"])
	}
}

func TestGuardedLogsPanicAndReturns(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	ran := false

	guarded(logger, "retention", func() { panic("job exploded") })
	guarded(logger, "daily figures", func() { ran = true })

	assert.True(t, ran)
	entries := logEntries(t, &buf)
	require.Len(t, entries, 1)
	assert.Equal(t, "ERROR", entries[0]["level"])
	assert.Equal(t, "retention", entries[0]["job"])
	assert.Equal(t, "job exploded", entries[0]["panic"])
	assert.Contains(t, entries[0]["stack"], "main_test.go")
}

func TestRunBoundsAnUnresponsiveDatabase(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	var held []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})

	attempt, deadline := openAttemptTimeout, openDeadline
	openAttemptTimeout, openDeadline = 100*time.Millisecond, 500*time.Millisecond
	t.Cleanup(func() { openAttemptTimeout, openDeadline = attempt, deadline })

	env := envOf(map[string]string{"DATABASE_URL": "postgres://hub@" + lis.Addr().String() + "/hub?sslmode=disable"})
	var stderr bytes.Buffer
	codes := make(chan int, 1)
	go func() { codes <- run(context.Background(), []string{"serve"}, env, io.Discard, &stderr) }()

	select {
	case code := <-codes:
		assert.Equal(t, 1, code)
		assert.Contains(t, "\n"+stderr.String(), "\nhub: open database")
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return")
	}
}
