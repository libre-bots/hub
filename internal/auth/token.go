package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Client is the authenticated caller.
type Client struct{ Bot string }

// HashToken returns the lower-case hex SHA-256 of token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken returns 32 random bytes, base64url without padding.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Tokens maps token hashes to bots. It never holds a token.
type Tokens struct {
	byHash map[string]string
}

// NewTokens takes a map of bot name to token hash.
func NewTokens(bots map[string]string) *Tokens {
	byHash := make(map[string]string, len(bots))
	for bot, hash := range bots {
		byHash[hash] = bot
	}
	return &Tokens{byHash: byHash}
}

// Lookup returns the bot that owns token.
func (t *Tokens) Lookup(token string) (Client, bool) {
	bot, ok := t.byHash[HashToken(token)]
	return Client{Bot: bot}, ok
}
