package auth

import (
	"testing"
	"time"

	"cryptoex/internal/models"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func sign(t *testing.T, method jwt.SigningMethod, claims jwt.Claims, secret string) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestParseRejectsTokenWithoutExpiry: a correctly signed token with no exp
// claim must not be accepted as a never-expiring session.
func TestParseRejectsTokenWithoutExpiry(t *testing.T) {
	m := NewManager("secret", 1)
	tok := sign(t, jwt.SigningMethodHS256, Claims{Role: "user", RegisteredClaims: jwt.RegisteredClaims{Subject: "7"}}, "secret")
	if _, _, err := m.Parse(tok); err == nil {
		t.Fatal("token without exp was accepted")
	}
}

// TestParseRejectsOtherHMACAlgorithms: only HS256 (what Issue signs with) is
// accepted.
func TestParseRejectsOtherHMACAlgorithms(t *testing.T) {
	m := NewManager("secret", 1)
	claims := Claims{Role: "user", RegisteredClaims: jwt.RegisteredClaims{
		Subject: "7", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	if _, _, err := m.Parse(sign(t, jwt.SigningMethodHS512, claims, "secret")); err == nil {
		t.Fatal("HS512 token was accepted")
	}
	if id, _, err := m.Parse(sign(t, jwt.SigningMethodHS256, claims, "secret")); err != nil || id != 7 {
		t.Fatalf("HS256 token: id=%d err=%v", id, err)
	}
}

// TestParseRejectsBotRole: a validly signed, unexpired token for the bot role
// must be refused, so bot sessions issued before the bot-login fix stop working.
func TestParseRejectsBotRole(t *testing.T) {
	m := NewManager("secret", 1)
	tok, err := m.Issue(1, models.RoleBot)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Parse(tok); err == nil {
		t.Fatal("bot-role token was accepted")
	}
}

// TestSpendPasswordCheckUsesValidHash guards the constant dummy hash: if it
// were malformed, bcrypt would fail fast and reintroduce the timing oracle.
func TestSpendPasswordCheckUsesValidHash(t *testing.T) {
	if _, err := bcrypt.Cost(dummyHash); err != nil {
		t.Fatalf("dummyHash is not a valid bcrypt hash: %v", err)
	}
	start := time.Now()
	SpendPasswordCheck("whatever-password")
	if time.Since(start) < 5*time.Millisecond {
		t.Fatal("dummy compare returned too fast to equalize timing")
	}
}
