package api

import (
	"net/http"

	"cryptoex/internal/auth"
	"cryptoex/internal/models"
)

func (s *Server) handleBalances(w http.ResponseWriter, r *http.Request) {
	bals, err := s.st.ListBalances(auth.UserID(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load balances")
		return
	}
	if bals == nil {
		bals = []models.Balance{}
	}
	writeJSON(w, http.StatusOK, bals)
}
