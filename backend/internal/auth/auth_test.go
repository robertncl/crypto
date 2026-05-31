package auth

import (
	"testing"
	"time"
)

func TestHashPassword(t *testing.T) {
	password := "test_password_123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}
	if hash == "" {
		t.Error("HashPassword() returned empty hash")
	}
	if hash == password {
		t.Error("HashPassword() should not return plaintext")
	}
}

func TestCheckPassword(t *testing.T) {
	password := "test_password_123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}

	if !CheckPassword(hash, password) {
		t.Error("CheckPassword() should return true for correct password")
	}

	if CheckPassword(hash, "wrong_password") {
		t.Error("CheckPassword() should return false for wrong password")
	}

	if CheckPassword(hash, "") {
		t.Error("CheckPassword() should return false for empty password")
	}
}

func TestHashPasswordEmpty(t *testing.T) {
	_, err := HashPassword("")
	if err != nil {
		t.Fatalf("HashPassword() with empty string should not error: %v", err)
	}
}

func TestNewManager(t *testing.T) {
	m := NewManager("secret", 24)
	if m == nil {
		t.Error("NewManager() returned nil")
	}
	if m.secret == nil {
		t.Error("NewManager() secret not set")
	}
	if m.ttl != 24*time.Hour {
		t.Errorf("NewManager() ttl = %v, want %v", m.ttl, 24*time.Hour)
	}
}

func TestManagerIssueAndParse(t *testing.T) {
	m := NewManager("test_secret_key", 24)
	userID := int64(12345)
	role := "user"

	token, err := m.Issue(userID, role)
	if err != nil {
		t.Fatalf("Issue() error: %v", err)
	}
	if token == "" {
		t.Error("Issue() returned empty token")
	}

	parsedID, parsedRole, err := m.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if parsedID != userID {
		t.Errorf("Parse() userID = %d, want %d", parsedID, userID)
	}
	if parsedRole != role {
		t.Errorf("Parse() role = %q, want %q", parsedRole, role)
	}
}

func TestManagerIssueMultipleRoles(t *testing.T) {
	m := NewManager("test_secret", 24)
	roles := []string{"admin", "user", "bot"}

	for _, role := range roles {
		token, err := m.Issue(999, role)
		if err != nil {
			t.Fatalf("Issue() error: %v", err)
		}
		_, parsedRole, _ := m.Parse(token)
		if parsedRole != role {
			t.Errorf("Parse() role = %q, want %q", parsedRole, role)
		}
	}
}

func TestManagerIssueWithDifferentSecrets(t *testing.T) {
	m1 := NewManager("secret1", 24)
	m2 := NewManager("secret2", 24)

	token, _ := m1.Issue(12345, "user")

	// m2 with different secret should fail to parse
	_, _, err := m2.Parse(token)
	if err == nil || err != ErrInvalidToken {
		t.Errorf("Parse() with wrong secret should return ErrInvalidToken, got %v", err)
	}
}

func TestManagerParseInvalidToken(t *testing.T) {
	m := NewManager("secret", 24)

	tests := []string{
		"",
		"invalid",
		"not.a.token",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.invalid.signature",
	}
	for _, token := range tests {
		_, _, err := m.Parse(token)
		if err != ErrInvalidToken {
			t.Errorf("Parse(%q) should return ErrInvalidToken, got %v", token, err)
		}
	}
}

func TestManagerParseExpiredToken(t *testing.T) {
	m := NewManager("secret", -1) // negative ttl for expired token
	token, _ := m.Issue(12345, "user")

	// Parse should fail for expired token
	_, _, err := m.Parse(token)
	if err != ErrInvalidToken {
		t.Errorf("Parse() expired token should return ErrInvalidToken, got %v", err)
	}
}

func TestManagerParseNegativeUserID(t *testing.T) {
	m := NewManager("secret", 24)
	token, _ := m.Issue(-1, "user")

	userID, _, err := m.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if userID != -1 {
		t.Errorf("Parse() userID = %d, want -1", userID)
	}
}

func TestManagerParseZeroUserID(t *testing.T) {
	m := NewManager("secret", 24)
	token, _ := m.Issue(0, "user")

	userID, _, err := m.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if userID != 0 {
		t.Errorf("Parse() userID = %d, want 0", userID)
	}
}

func TestManagerMultipleTokens(t *testing.T) {
	m := NewManager("secret", 24)

	// Generate multiple tokens for same user with different roles
	token1, _ := m.Issue(100, "user")
	token2, _ := m.Issue(100, "admin")

	id1, role1, _ := m.Parse(token1)
	id2, role2, _ := m.Parse(token2)

	if id1 != id2 {
		t.Error("Different tokens for same user should have same ID")
	}
	if role1 == role2 {
		t.Error("Different tokens should have different roles")
	}
}

func TestManagerLargeUserID(t *testing.T) {
	m := NewManager("secret", 24)
	largeID := int64(9223372036854775807) // max int64

	token, _ := m.Issue(largeID, "user")
	parsedID, _, err := m.Parse(token)

	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if parsedID != largeID {
		t.Errorf("Parse() userID = %d, want %d", parsedID, largeID)
	}
}

func TestClaimsRegisteredClaims(t *testing.T) {
	m := NewManager("secret", 24)
	token, _ := m.Issue(12345, "user")

	_, _, err := m.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	// If parse succeeds, the token structure is valid
}
