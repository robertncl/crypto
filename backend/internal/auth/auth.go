// Package auth handles password hashing and stateless JWT access tokens.
package auth

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidToken = errors.New("invalid token")

// MinPasswordLen and MaxPasswordLen bound accepted passwords. bcrypt only uses
// the first 72 bytes (and rejects longer input), so longer passwords are
// refused up front rather than failing or being silently truncated.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 72
)

// dummyHash is a bcrypt hash of a throwaway secret, computed on first use.
var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("nebula-login-timing-equalizer"), bcrypt.DefaultCost)
	return h
})

// SpendPasswordCheck runs a bcrypt comparison against a dummy hash and discards
// the result. Login calls it when no account matches, so a miss costs the same
// time as a wrong password and response timing does not reveal which emails
// are registered.
func SpendPasswordCheck(pw string) {
	if len(pw) > MaxPasswordLen {
		pw = pw[:MaxPasswordLen]
	}
	_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(pw))
}

// HashPassword returns a bcrypt hash of the plaintext password.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword reports whether pw matches the stored bcrypt hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// Claims is the JWT payload: the subject is the user id, with a role for
// coarse authorization.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// Manager issues and verifies tokens with a shared HMAC secret.
type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, ttlHours int) *Manager {
	return &Manager{secret: []byte(secret), ttl: time.Duration(ttlHours) * time.Hour}
}

// Issue creates a signed token for the given user.
func (m *Manager) Issue(userID int64, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Parse validates a token's signature and expiry and returns the user id and role.
// Only HS256 (the algorithm Issue uses) is accepted, and a token without an
// expiry is rejected rather than treated as never-expiring.
func (m *Manager) Parse(tokenStr string) (userID int64, role string, err error) {
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !tok.Valid {
		return 0, "", ErrInvalidToken
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, "", ErrInvalidToken
	}
	return id, claims.Role, nil
}
