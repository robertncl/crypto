package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
