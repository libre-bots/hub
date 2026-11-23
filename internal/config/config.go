package config

import (
	"fmt"
	"regexp"
	"strings"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string
	Bots        map[string]string // bot name -> lower-case hex SHA-256 of its token
	Reflection  bool
	TLSCertFile string
	TLSKeyFile  string
}

var (
	botName  = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)
	tokenHex = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Load reads the configuration through getenv and readFile, never from os.
func Load(getenv func(string) string, readFile func(string) ([]byte, error)) (Config, error) {
	cfg := Config{
		ListenAddr:  getenv("HUB_LISTEN_ADDR"),
		TLSCertFile: getenv("HUB_TLS_CERT_FILE"),
		TLSKeyFile:  getenv("HUB_TLS_KEY_FILE"),
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":50051"
	}

	url, urlFile := getenv("DATABASE_URL"), getenv("DATABASE_URL_FILE")
	switch {
	case url != "" && urlFile != "":
		return Config{}, fmt.Errorf("DATABASE_URL: set either DATABASE_URL or DATABASE_URL_FILE, not both")
	case url == "" && urlFile == "":
		return Config{}, fmt.Errorf("DATABASE_URL: set either DATABASE_URL or DATABASE_URL_FILE")
	case urlFile != "":
		content, err := readFile(urlFile)
		if err != nil {
			return Config{}, fmt.Errorf("DATABASE_URL_FILE: %w", err)
		}
		url = strings.TrimSpace(string(content))
		if url == "" {
			return Config{}, fmt.Errorf("DATABASE_URL_FILE: file is empty")
		}
	}
	cfg.DatabaseURL = url

	bots, err := parseBots(getenv("HUB_BOT_TOKENS"))
	if err != nil {
		return Config{}, err
	}
	cfg.Bots = bots

	switch getenv("HUB_REFLECTION") {
	case "", "false":
	case "true":
		cfg.Reflection = true
	default:
		return Config{}, fmt.Errorf("HUB_REFLECTION: must be true or false")
	}

	if cfg.TLSCertFile != "" && cfg.TLSKeyFile == "" {
		return Config{}, fmt.Errorf("HUB_TLS_KEY_FILE: required when HUB_TLS_CERT_FILE is set")
	}
	if cfg.TLSKeyFile != "" && cfg.TLSCertFile == "" {
		return Config{}, fmt.Errorf("HUB_TLS_CERT_FILE: required when HUB_TLS_KEY_FILE is set")
	}
	return cfg, nil
}

func parseBots(value string) (map[string]string, error) {
	bots := map[string]string{}
	if strings.TrimSpace(value) == "" {
		return bots, nil
	}
	owners := map[string]string{}
	for i, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		name, hash, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("HUB_BOT_TOKENS: entry %d has no =", i+1)
		}
		if !botName.MatchString(name) || name == "all" {
			return nil, fmt.Errorf("HUB_BOT_TOKENS: entry %d has an invalid bot name", i+1)
		}
		if !tokenHex.MatchString(hash) {
			return nil, fmt.Errorf("HUB_BOT_TOKENS: hash of bot %s is not 64 lower-case hex characters", name)
		}
		if _, dup := bots[name]; dup {
			return nil, fmt.Errorf("HUB_BOT_TOKENS: bot %s is listed twice", name)
		}
		if other, dup := owners[hash]; dup {
			return nil, fmt.Errorf("HUB_BOT_TOKENS: bot %s has the same hash as bot %s", name, other)
		}
		bots[name] = hash
		owners[hash] = name
	}
	return bots, nil
}
