package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearer(t *testing.T) {
	tests := []struct {
		name   string
		header string
		query  string
		want   string
	}{
		{"standard bearer", "Bearer abc123", "", "abc123"},
		{"lowercase bearer", "bearer abc123", "", "abc123"},
		{"mixed case", "BeArEr abc123", "", "abc123"},
		{"trims surrounding spaces", "Bearer   abc123  ", "", "abc123"},
		{"no header falls back to query", "", "querytoken", "querytoken"},
		{"header takes precedence over query", "Bearer headertoken", "querytoken", "headertoken"},
		{"empty everything", "", "", ""},
		{"non-bearer scheme ignored", "Basic abc123", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/?token="+tt.query, nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			if got := bearer(r); got != tt.want {
				t.Errorf("bearer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUserIDFromContext(t *testing.T) {
	if got := UserID(context.Background()); got != 0 {
		t.Errorf("UserID(empty) = %d, want 0", got)
	}
	ctx := context.WithValue(context.Background(), userIDKey, int64(42))
	if got := UserID(ctx); got != 42 {
		t.Errorf("UserID() = %d, want 42", got)
	}
}

func TestRoleFromContext(t *testing.T) {
	if got := Role(context.Background()); got != "" {
		t.Errorf("Role(empty) = %q, want empty", got)
	}
	ctx := context.WithValue(context.Background(), roleKey, "admin")
	if got := Role(ctx); got != "admin" {
		t.Errorf("Role() = %q, want admin", got)
	}
}

func TestMiddleware(t *testing.T) {
	m := NewManager("test-secret", 24)
	token, err := m.Issue(99, "user")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	var gotID int64
	var gotRole string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = UserID(r.Context())
		gotRole = Role(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := m.Middleware(next)

	t.Run("valid token injects context", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if gotID != 99 || gotRole != "user" {
			t.Errorf("context id=%d role=%q, want 99/user", gotID, gotRole)
		}
	})

	t.Run("missing token is 401", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("invalid token is 401", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer not-a-real-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("token via query param", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/?token="+token, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 (query-param token)", w.Code)
		}
	})
}
