package api

import (
	"net/http"
	"strings"

	"cryptoex/internal/auth"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
)

func (s *Server) handleWalletAddress(w http.ResponseWriter, r *http.Request) {
	asset := strings.ToUpper(r.URL.Query().Get("asset"))
	addr, err := s.wallet.Address(auth.UserID(r.Context()), asset)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, addr)
}

type depositReq struct {
	Asset  string  `json:"asset"`
	Amount num.Dec `json:"amount"`
}

func (s *Server) handleDeposit(w http.ResponseWriter, r *http.Request) {
	var req depositReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	txn, err := s.wallet.Deposit(auth.UserID(r.Context()), strings.ToUpper(req.Asset), req.Amount)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, txn)
}

type withdrawReq struct {
	Asset   string  `json:"asset"`
	Address string  `json:"address"`
	Amount  num.Dec `json:"amount"`
}

func (s *Server) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	var req withdrawReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	txn, err := s.wallet.Withdraw(auth.UserID(r.Context()), strings.ToUpper(req.Asset), req.Address, req.Amount)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, txn)
}

func (s *Server) handleWalletTxns(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 100, 500)
	txns, err := s.st.ListTxns(auth.UserID(r.Context()), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load transactions")
		return
	}
	if txns == nil {
		txns = []models.WalletTxn{}
	}
	writeJSON(w, http.StatusOK, txns)
}
