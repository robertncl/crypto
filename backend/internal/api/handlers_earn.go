package api

import (
	"net/http"
	"strings"

	"cryptoex/internal/auth"
	"cryptoex/internal/models"
	"cryptoex/internal/num"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleEarnProducts(w http.ResponseWriter, _ *http.Request) {
	products, err := s.earn.Products()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load earn products")
		return
	}
	if products == nil {
		products = []models.EarnProduct{}
	}
	writeJSON(w, http.StatusOK, products)
}

func (s *Server) handleEarnPositions(w http.ResponseWriter, r *http.Request) {
	positions, err := s.earn.Positions(auth.UserID(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load earn positions")
		return
	}
	if positions == nil {
		positions = []models.EarnPosition{}
	}
	writeJSON(w, http.StatusOK, positions)
}

type earnSubscribeReq struct {
	ProductID string  `json:"productId"`
	Amount    num.Dec `json:"amount"`
}

func (s *Server) handleEarnSubscribe(w http.ResponseWriter, r *http.Request) {
	var req earnSubscribeReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pos, err := s.earn.Subscribe(auth.UserID(r.Context()), strings.ToUpper(req.ProductID), req.Amount)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, pos)
}

func (s *Server) handleEarnRedeem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	pos, err := s.earn.Redeem(auth.UserID(r.Context()), id)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pos)
}
