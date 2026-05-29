package api

import (
	"net/http"
	"strings"
	"time"

	"cryptoex/internal/auth"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string       `json:"token"`
	User  *models.User `json:"user"`
}

// welcomeBonus is credited to new accounts so the demo is usable immediately.
var welcomeBonus = num.MustParse("10000")

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := decode(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	if !strings.Contains(c.Email, "@") || len(c.Email) < 3 {
		writeErr(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if len(c.Password) < 6 {
		writeErr(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}
	if _, err := s.st.GetUserByEmail(c.Email); err == nil {
		writeErr(w, http.StatusConflict, "an account with that email already exists")
		return
	}
	hash, err := auth.HashPassword(c.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not hash password")
		return
	}
	user, err := s.st.CreateUser(c.Email, hash, "user", time.Now().Unix())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not create account")
		return
	}
	// Grant a demo welcome balance.
	_ = s.st.ApplyPostings("welcome:"+itoa(user.ID), time.Now().Unix(), []store.Posting{{
		UserID: user.ID, Asset: "USDT", DeltaAvailable: welcomeBonus, Reason: "welcome_bonus", Ref: "signup",
	}})

	s.issue(w, user)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := decode(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	user, err := s.st.GetUserByEmail(c.Email)
	if err != nil || !auth.CheckPassword(user.PasswordHash, c.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	s.issue(w, user)
}

func (s *Server) issue(w http.ResponseWriter, user *models.User) {
	token, err := s.auth.Issue(user.ID, user.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, authResponse{Token: token, User: user})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := s.st.GetUserByID(auth.UserID(r.Context()))
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// handleKYCVerify is a demo stub that instantly marks the account verified,
// unlocking withdrawals.
func (s *Server) handleKYCVerify(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	if err := s.st.SetKYCStatus(uid, "verified"); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not update status")
		return
	}
	user, _ := s.st.GetUserByID(uid)
	writeJSON(w, http.StatusOK, user)
}

func itoa(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
