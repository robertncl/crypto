package config

import "testing"

// envKeys are all variables Load reads; clearing them isolates default tests.
var envKeys = []string{
	"ADDR", "DB_PATH", "JWT_SECRET", "JWT_TTL_HOURS",
	"ENABLE_BOT", "CORS_ORIGIN", "WEB_DIR", "PERP_FUNDING_SEC", "DEV",
}

func TestLoadDefaults(t *testing.T) {
	for _, k := range envKeys {
		t.Setenv(k, "") // empty is treated as "unset" by all the env helpers
	}
	c := Load()
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.DBPath != "exchange.db" {
		t.Errorf("DBPath = %q, want exchange.db", c.DBPath)
	}
	if c.JWTSecret != DefaultJWTSecret {
		t.Errorf("JWTSecret = %q, want default", c.JWTSecret)
	}
	if c.JWTTTLHours != 72 {
		t.Errorf("JWTTTLHours = %d, want 72", c.JWTTTLHours)
	}
	if !c.EnableBot {
		t.Error("EnableBot should default to true")
	}
	if c.CORSOrigin != "http://localhost:5173" {
		t.Errorf("CORSOrigin = %q", c.CORSOrigin)
	}
	if c.PerpFunding != 60 {
		t.Errorf("PerpFunding = %d, want 60", c.PerpFunding)
	}
	if c.Dev {
		t.Error("Dev should default to false")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("ADDR", ":9000")
	t.Setenv("DB_PATH", "/tmp/custom.db")
	t.Setenv("JWT_SECRET", "my-secret")
	t.Setenv("JWT_TTL_HOURS", "24")
	t.Setenv("ENABLE_BOT", "false")
	t.Setenv("DEV", "true")

	c := Load()
	if c.Addr != ":9000" {
		t.Errorf("Addr = %q, want :9000", c.Addr)
	}
	if c.DBPath != "/tmp/custom.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
	if c.JWTSecret != "my-secret" {
		t.Errorf("JWTSecret = %q", c.JWTSecret)
	}
	if c.JWTTTLHours != 24 {
		t.Errorf("JWTTTLHours = %d, want 24", c.JWTTTLHours)
	}
	if c.EnableBot {
		t.Error("EnableBot should be false")
	}
	if !c.Dev {
		t.Error("Dev should be true")
	}
}

func TestEnvString(t *testing.T) {
	t.Setenv("MY_STR", "hello")
	if got := env("MY_STR", "def"); got != "hello" {
		t.Errorf("env() = %q, want hello", got)
	}
	if got := env("MISSING_STR", "def"); got != "def" {
		t.Errorf("env() default = %q, want def", got)
	}
	t.Setenv("EMPTY_STR", "")
	if got := env("EMPTY_STR", "def"); got != "def" {
		t.Errorf("env() empty = %q, want def (empty treated as unset)", got)
	}
}

func TestEnvInt(t *testing.T) {
	t.Setenv("MY_INT", "42")
	if got := envInt("MY_INT", 0); got != 42 {
		t.Errorf("envInt() = %d, want 42", got)
	}
	if got := envInt("MISSING_INT", 7); got != 7 {
		t.Errorf("envInt() default = %d, want 7", got)
	}
	t.Setenv("BAD_INT", "not-a-number")
	if got := envInt("BAD_INT", 7); got != 7 {
		t.Errorf("envInt() invalid = %d, want fallback 7", got)
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("MY_BOOL", "true")
	if !envBool("MY_BOOL", false) {
		t.Error("envBool() = false, want true")
	}
	t.Setenv("MY_BOOL_0", "0")
	if envBool("MY_BOOL_0", true) {
		t.Error("envBool(0) = true, want false")
	}
	if !envBool("MISSING_BOOL", true) {
		t.Error("envBool() default = false, want true")
	}
	t.Setenv("BAD_BOOL", "maybe")
	if !envBool("BAD_BOOL", true) {
		t.Error("envBool() invalid = false, want fallback true")
	}
}
