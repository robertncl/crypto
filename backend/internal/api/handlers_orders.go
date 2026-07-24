package api

import (
	"net/http"
	"strings"

	"cryptoex/internal/auth"
	"cryptoex/internal/models"
	"cryptoex/internal/num"

	"github.com/go-chi/chi/v5"
)

type placeOrderReq struct {
	Market   string  `json:"market"`
	Side     string  `json:"side"`
	Type     string  `json:"type"`
	Price    num.Dec `json:"price"`
	Quantity num.Dec `json:"quantity"`
}

func (s *Server) handlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	var req placeOrderReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	side, typ, ok := parseSideType(w, req.Side, req.Type)
	if !ok {
		return
	}
	eng, ok := s.mgr.Get(strings.ToUpper(req.Market))
	if !ok {
		writeErr(w, http.StatusNotFound, "market not found")
		return
	}
	order := &models.Order{
		UserID:   auth.UserID(r.Context()),
		Side:     side,
		Type:     typ,
		Price:    req.Price,
		Quantity: req.Quantity,
	}
	out, err := eng.Place(order)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	order, err := s.st.GetOrder(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "order not found")
		return
	}
	eng, ok := s.mgr.Get(order.Market)
	if !ok {
		writeErr(w, http.StatusNotFound, "market not found")
		return
	}
	if err := eng.Cancel(id, auth.UserID(r.Context())); err != nil {
		writeDomainErr(w, err)
		return
	}
	out, _ := s.st.GetOrder(id)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleOpenOrders(w http.ResponseWriter, r *http.Request) {
	market := strings.ToUpper(r.URL.Query().Get("market"))
	orders, err := s.st.ListOpenOrders(auth.UserID(r.Context()), market)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load orders")
		return
	}
	writeJSON(w, http.StatusOK, nonNilOrders(orders))
}

func (s *Server) handleOrderHistory(w http.ResponseWriter, r *http.Request) {
	market := strings.ToUpper(r.URL.Query().Get("market"))
	limit := queryInt(r, "limit", 100, 500)
	orders, err := s.st.ListOrderHistory(auth.UserID(r.Context()), market, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load order history")
		return
	}
	writeJSON(w, http.StatusOK, nonNilOrders(orders))
}

func (s *Server) handleMyTrades(w http.ResponseWriter, r *http.Request) {
	market := strings.ToUpper(r.URL.Query().Get("market"))
	limit := queryInt(r, "limit", 100, 500)
	trades, err := s.st.ListTradesByUser(auth.UserID(r.Context()), market, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load trades")
		return
	}
	if trades == nil {
		trades = []models.Trade{}
	}
	writeJSON(w, http.StatusOK, trades)
}

func nonNilOrders(o []models.Order) []models.Order {
	if o == nil {
		return []models.Order{}
	}
	return o
}
