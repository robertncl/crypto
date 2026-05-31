// Package config loads runtime configuration from environment variables with
// sensible development defaults so the server runs with zero setup.
package config

import (
	"os"
	"strconv"
)

// DefaultJWTSecret is the insecure development secret. Using it outside dev mode
// is refused at startup (see cmd/server) because it would allow token forgery.
const DefaultJWTSecret = "dev-insecure-secret-change-me"

type Config struct {
	Addr        string // HTTP listen address, e.g. ":8080"
	DBPath      string // SQLite file path
	JWTSecret   string // HMAC secret for signing access tokens
	JWTTTLHours int    // access token lifetime in hours
	EnableBot   bool   // run the seed market-maker bot for a lively demo
	CORSOrigin  string // allowed CORS origin for the SPA dev server
	WebDir      string // directory of the built SPA to serve (empty = skip)
	PerpFunding int    // perp funding interval in seconds
	Dev         bool   // local development mode (relaxes the JWT-secret guard)
}

func Load() Config {
	return Config{
		Addr:        env("ADDR", ":8080"),
		DBPath:      env("DB_PATH", "exchange.db"),
		JWTSecret:   env("JWT_SECRET", DefaultJWTSecret),
		JWTTTLHours: envInt("JWT_TTL_HOURS", 72),
		EnableBot:   envBool("ENABLE_BOT", true),
		CORSOrigin:  env("CORS_ORIGIN", "http://localhost:5173"),
		WebDir:      env("WEB_DIR", "web/dist"),
		PerpFunding: envInt("PERP_FUNDING_SEC", 60),
		Dev:         envBool("DEV", false),
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
