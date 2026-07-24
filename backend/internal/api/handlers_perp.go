package api

import (
	"net/http"
	"strings"

	"cryptoex/internal/auth"
	"cryptoex/internal/models"
	"cryptoex/internal/num"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handlePerpMarkets(w http.ResponseWriter, _ *http.Request) {
	markets, err := s.st.ListPerpMarkets()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load perp markets")
		return
	}
	if markets == nil {
		markets = []models.PerpMarket{}
	}
	writeJSON(w, http.StatusOK, markets)
}

func (s *Server) handlePerpMarket(w http.ResponseWriter, r *http.Request) {
	m, err := s.st.GetPerpMarket(symbolParam(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, "market not found")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handlePerpDepth(w http.ResponseWriter, r *http.Request) {
	eng, ok := s.perp.Get(symbolParam(r))
	if !ok {
		writeErr(w, http.StatusNotFound, "market not found")
		return
	}
	writeJSON(w, http.StatusOK, eng.Depth(queryInt(r, "limit", 50, 200)))
}

func (s *Server) handlePerpFunding(w http.ResponseWriter, r *http.Request) {
	sym := symbolParam(r)
	fi, ok := s.perp.Funding(sym)
	if !ok {
		// Not computed yet — return a zero-rate snapshot with live prices.
		fi = models.FundingInfo{Market: sym, MarkPrice: s.perp.MarkPrice(sym), IndexPrice: s.perp.IndexPrice(sym)}
	}
	writeJSON(w, http.StatusOK, fi)
}

type placePerpReq struct {
	Market     string  `json:"market"`
	Side       string  `json:"side"`
	Type       string  `json:"type"`
	Price      num.Dec `json:"price"`
	Quantity   num.Dec `json:"quantity"`
	Leverage   int     `json:"leverage"`
	ReduceOnly bool    `json:"reduceOnly"`
}

func (s *Server) handlePlacePerpOrder(w http.ResponseWriter, r *http.Request) {
	var req placePerpReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	side, typ, ok := parseSideType(w, req.Side, req.Type)
	if !ok {
		return
	}
	eng, ok := s.perp.Get(strings.ToUpper(req.Market))
	if !ok {
		writeErr(w, http.StatusNotFound, "market not found")
		return
	}
	out, err := eng.Place(&models.PerpOrder{
		UserID: auth.UserID(r.Context()), Side: side, Type: typ,
		Price: req.Price, Quantity: req.Quantity, Leverage: req.Leverage, ReduceOnly: req.ReduceOnly,
	})
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleCancelPerpOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	order, err := s.st.GetPerpOrder(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "order not found")
		return
	}
	eng, ok := s.perp.Get(order.Market)
	if !ok {
		writeErr(w, http.StatusNotFound, "market not found")
		return
	}
	if err := eng.Cancel(id, auth.UserID(r.Context())); err != nil {
		writeDomainErr(w, err)
		return
	}
	out, _ := s.st.GetPerpOrder(id)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleOpenPerpOrders(w http.ResponseWriter, r *http.Request) {
	market := strings.ToUpper(r.URL.Query().Get("market"))
	orders, err := s.st.ListOpenPerpOrders(auth.UserID(r.Context()), market)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load orders")
		return
	}
	if orders == nil {
		orders = []models.PerpOrder{}
	}
	writeJSON(w, http.StatusOK, orders)
}

func (s *Server) handlePerpOrderHistory(w http.ResponseWriter, r *http.Request) {
	market := strings.ToUpper(r.URL.Query().Get("market"))
	orders, err := s.st.ListPerpOrderHistory(auth.UserID(r.Context()), market, queryInt(r, "limit", 100, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load order history")
		return
	}
	if orders == nil {
		orders = []models.PerpOrder{}
	}
	writeJSON(w, http.StatusOK, orders)
}

func (s *Server) handlePositions(w http.ResponseWriter, r *http.Request) {
	positions, err := s.st.ListPositions(auth.UserID(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load positions")
		return
	}
	out := make([]models.Position, 0, len(positions))
	for _, p := range positions {
		if eng, ok := s.perp.Get(p.Market); ok {
			out = append(out, eng.Enrich(p, s.perp.MarkPrice(p.Market)))
		} else {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleClosePosition market-closes the caller's position with a reduce-only order.
func (s *Server) handleClosePosition(w http.ResponseWriter, r *http.Request) {
	sym := symbolParam(r)
	uid := auth.UserID(r.Context())
	pos, err := s.st.GetPosition(uid, sym)
	if err != nil || pos.Side == models.Flat || pos.Size.Sign() <= 0 {
		writeErr(w, http.StatusBadRequest, "no open position to close")
		return
	}
	eng, ok := s.perp.Get(sym)
	if !ok {
		writeErr(w, http.StatusNotFound, "market not found")
		return
	}
	// Opposite side reduces the position.
	side := models.Sell
	if pos.Side == models.Short {
		side = models.Buy
	}
	out, err := eng.Place(&models.PerpOrder{
		UserID: uid, Side: side, Type: models.TypeMarket,
		Quantity: pos.Size, Leverage: pos.Leverage, ReduceOnly: true,
	})
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
