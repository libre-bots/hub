package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/libre-bots/hub/internal/config"
)

var (
	hashA = strings.Repeat("a", 64)
	hashB = strings.Repeat("b", 64)
)

func TestLoad(t *testing.T) {
	files := map[string]string{"/run/secrets/db": "postgres://x\n"}
	readFile := func(path string) ([]byte, error) {
		content, ok := files[path]
		if !ok {
			return nil, errors.New("not found")
		}
		return []byte(content), nil
	}

	tests := []struct {
		name    string
		env     map[string]string
		check   func(t *testing.T, cfg config.Config)
		wantErr string
	}{
		{
			name: "defaults",
			env:  map[string]string{"DATABASE_URL": "postgres://y"},
			check: func(t *testing.T, cfg config.Config) {
				require.Equal(t, ":50051", cfg.ListenAddr)
				require.Equal(t, "postgres://y", cfg.DatabaseURL)
				require.False(t, cfg.Reflection)
				require.Len(t, cfg.Bots, 0)
			},
		},
		{
			name: "database url from file",
			env:  map[string]string{"DATABASE_URL_FILE": "/run/secrets/db"},
			check: func(t *testing.T, cfg config.Config) {
				require.Equal(t, "postgres://x", cfg.DatabaseURL)
			},
		},
		{
			name:    "database url file unreadable",
			env:     map[string]string{"DATABASE_URL_FILE": "/missing"},
			wantErr: "DATABASE_URL_FILE",
		},
		{
			name:    "both database url variables",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "DATABASE_URL_FILE": "/run/secrets/db"},
			wantErr: "DATABASE_URL",
		},
		{
			name:    "no database url",
			env:     map[string]string{},
			wantErr: "DATABASE_URL",
		},
		{
			name: "two bots",
			env: map[string]string{
				"DATABASE_URL":   "postgres://y",
				"HUB_BOT_TOKENS": "starboard=" + hashA + ", tickets=" + hashB,
			},
			check: func(t *testing.T, cfg config.Config) {
				require.Equal(t, map[string]string{"starboard": hashA, "tickets": hashB}, cfg.Bots)
			},
		},
		{
			name:    "bot name with upper case",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "All=" + hashA},
			wantErr: "HUB_BOT_TOKENS",
		},
		{
			name:    "bot name all",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "all=" + hashA},
			wantErr: "HUB_BOT_TOKENS",
		},
		{
			name:    "bot name too short",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "a=" + hashA},
			wantErr: "HUB_BOT_TOKENS",
		},
		{
			name:    "bot name starts with digit",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "1x=" + hashA},
			wantErr: "HUB_BOT_TOKENS",
		},
		{
			name:    "hash too short",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "alpha=" + hashA[:63]},
			wantErr: "alpha",
		},
		{
			name:    "hash with upper case",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "alpha=" + strings.Repeat("A", 64)},
			wantErr: "alpha",
		},
		{
			name:    "entry without equals sign",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "alpha"},
			wantErr: "HUB_BOT_TOKENS",
		},
		{
			name:    "repeated name",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "alpha=" + hashA + ",alpha=" + hashB},
			wantErr: "alpha",
		},
		{
			name:    "repeated hash",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "alpha=" + hashA + ",beta=" + hashA},
			wantErr: "beta",
		},
		{
			name: "both tls variables",
			env: map[string]string{
				"DATABASE_URL": "postgres://y", "HUB_TLS_CERT_FILE": "/c.pem", "HUB_TLS_KEY_FILE": "/k.pem",
			},
			check: func(t *testing.T, cfg config.Config) {
				require.Equal(t, "/c.pem", cfg.TLSCertFile)
				require.Equal(t, "/k.pem", cfg.TLSKeyFile)
			},
		},
		{
			name:    "only tls cert",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_TLS_CERT_FILE": "/c.pem"},
			wantErr: "HUB_TLS_KEY_FILE",
		},
		{
			name:    "only tls key",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_TLS_KEY_FILE": "/k.pem"},
			wantErr: "HUB_TLS_CERT_FILE",
		},
		{
			name: "reflection true",
			env:  map[string]string{"DATABASE_URL": "postgres://y", "HUB_REFLECTION": "true"},
			check: func(t *testing.T, cfg config.Config) {
				require.True(t, cfg.Reflection)
			},
		},
		{
			name:    "reflection yes",
			env:     map[string]string{"DATABASE_URL": "postgres://y", "HUB_REFLECTION": "yes"},
			wantErr: "HUB_REFLECTION",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load(func(key string) string { return tt.env[key] }, readFile)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

func TestEntryWithoutEqualsSignIsNotEchoed(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://y", "HUB_BOT_TOKENS": "alpha=" + hashA + ", pasted-secret-token"}
	_, err := config.Load(func(key string) string { return env[key] }, func(string) ([]byte, error) { return nil, errors.New("not found") })
	require.ErrorContains(t, err, "HUB_BOT_TOKENS: entry 2 has no =")
	require.NotContains(t, err.Error(), "pasted-secret-token")
}
