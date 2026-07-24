package store

import (
	"database/sql"
	"errors"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
)

// InsuranceFundID is the reserved account that acts as the clearing counterparty
// for realized PnL, funding, and liquidation shortfalls. Because every fill
// creates equal-and-opposite long/short deltas, aggregate position PnL nets to
// zero, so this fund stays near its seeded value over time.
const InsuranceFundID int64 = -2

// ---------- perp markets ----------

func (s *Store) ListPerpMarkets() ([]models.PerpMarket, error) {
	rows, err := s.db.Query(`SELECT symbol,base,settle,index_symbol,price_tick,qty_step,min_notional,maker_fee,taker_fee,max_leverage,mmr,status FROM perp_markets ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PerpMarket
	for rows.Next() {
		m, err := scanPerpMarket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *Store) GetPerpMarket(symbol string) (*models.PerpMarket, error) {
	row := s.db.QueryRow(`SELECT symbol,base,settle,index_symbol,price_tick,qty_step,min_notional,maker_fee,taker_fee,max_leverage,mmr,status FROM perp_markets WHERE symbol=?`, symbol)
	return scanPerpMarket(row)
}

type scanner interface{ Scan(...any) error }

func scanPerpMarket(sc scanner) (*models.PerpMarket, error) {
	var m models.PerpMarket
	var pt, qs, mn, mf, tf, mmr int64
	err := sc.Scan(&m.Symbol, &m.Base, &m.Settle, &m.IndexSymbol, &pt, &qs, &mn, &mf, &tf, &m.MaxLeverage, &mmr, &m.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.PriceTick, m.QtyStep, m.MinNotional = num.FromRaw(pt), num.FromRaw(qs), num.FromRaw(mn)
	m.MakerFee, m.TakerFee, m.MMR = num.FromRaw(mf), num.FromRaw(tf), num.FromRaw(mmr)
	return &m, nil
}

// ---------- positions ----------

const posCols = `SELECT id,user_id,market,side,size,entry_price,margin,leverage,realized_pnl,funding_paid,updated_at FROM positions`

func scanPosition(sc scanner) (*models.Position, error) {
	var p models.Position
	var size, entry, margin, rpnl, funding int64
	err := sc.Scan(&p.ID, &p.UserID, &p.Market, &p.Side, &size, &entry, &margin, &p.Leverage, &rpnl, &funding, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Size, p.EntryPrice, p.Margin = num.FromRaw(size), num.FromRaw(entry), num.FromRaw(margin)
	p.RealizedPnL, p.FundingPaid = num.FromRaw(rpnl), num.FromRaw(funding)
	return &p, nil
}

// GetPosition returns the user's position in a market, or a flat zero-value
// position (not an error) when none exists.
func (s *Store) GetPosition(userID int64, market string) (*models.Position, error) {
	row := s.db.QueryRow(posCols+` WHERE user_id=? AND market=?`, userID, market)
	p, err := scanPosition(row)
	if errors.Is(err, sql.ErrNoRows) {
		return &models.Position{UserID: userID, Market: market, Side: models.Flat}, nil
	}
	return p, err
}

// ListPositions returns the user's open positions (size > 0).
func (s *Store) ListPositions(userID int64) ([]models.Position, error) {
	rows, err := s.db.Query(posCols+` WHERE user_id=? AND size>0 ORDER BY market`, userID)
	if err != nil {
		return nil, err
	}
	return scanPositions(rows)
}

// ListOpenPositionsByMarket returns all open positions in a market (for funding
// and liquidation sweeps).
func (s *Store) ListOpenPositionsByMarket(market string) ([]models.Position, error) {
	rows, err := s.db.Query(posCols+` WHERE market=? AND size>0`, market)
	if err != nil {
		return nil, err
	}
	return scanPositions(rows)
}

func scanPositions(rows *sql.Rows) ([]models.Position, error) {
	defer rows.Close()
	var out []models.Position
	for rows.Next() {
		p, err := scanPosition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ---------- perp orders ----------

const perpOrderCols = `SELECT id,user_id,market,side,type,price,quantity,filled,avg_price,leverage,reduce_only,status,created_at,updated_at FROM perp_orders`

func scanPerpOrder(sc scanner) (*models.PerpOrder, error) {
	var o models.PerpOrder
	var price, qty, filled, avg int64
	var reduce int
	err := sc.Scan(&o.ID, &o.UserID, &o.Market, &o.Side, &o.Type, &price, &qty, &filled, &avg, &o.Leverage, &reduce, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	o.Price, o.Quantity, o.Filled, o.AvgPrice = num.FromRaw(price), num.FromRaw(qty), num.FromRaw(filled), num.FromRaw(avg)
	o.ReduceOnly = reduce != 0
	return &o, nil
}

func scanPerpOrders(rows *sql.Rows) ([]models.PerpOrder, error) {
	defer rows.Close()
	var out []models.PerpOrder
	for rows.Next() {
		o, err := scanPerpOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func (s *Store) InsertPerpOrder(o *models.PerpOrder) error {
	_, err := s.db.Exec(
		`INSERT INTO perp_orders(id,user_id,market,side,type,price,quantity,filled,avg_price,leverage,reduce_only,status,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.UserID, o.Market, o.Side, o.Type, o.Price.Raw(), o.Quantity.Raw(), o.Filled.Raw(),
		o.AvgPrice.Raw(), o.Leverage, b2i(o.ReduceOnly), o.Status, o.CreatedAt, o.UpdatedAt)
	return err
}

func (s *Store) UpdatePerpOrder(o *models.PerpOrder) error {
	_, err := s.db.Exec(
		`UPDATE perp_orders SET filled=?, avg_price=?, status=?, updated_at=? WHERE id=?`,
		o.Filled.Raw(), o.AvgPrice.Raw(), o.Status, o.UpdatedAt, o.ID)
	return err
}

func (s *Store) GetPerpOrder(id string) (*models.PerpOrder, error) {
	return scanPerpOrder(s.db.QueryRow(perpOrderCols+` WHERE id=?`, id))
}

func (s *Store) ListOpenPerpOrders(userID int64, market string) ([]models.PerpOrder, error) {
	q := perpOrderCols + ` WHERE user_id=? AND status IN ('open','partial')`
	args := []any{userID}
	if market != "" {
		q += ` AND market=?`
		args = append(args, market)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return scanPerpOrders(rows)
}

func (s *Store) ListPerpOrderHistory(userID int64, market string, limit int) ([]models.PerpOrder, error) {
	q := perpOrderCols + ` WHERE user_id=?`
	args := []any{userID}
	if market != "" {
		q += ` AND market=?`
		args = append(args, market)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return scanPerpOrders(rows)
}

func (s *Store) ListWorkingPerpOrdersByMarket(market string) ([]models.PerpOrder, error) {
	rows, err := s.db.Query(perpOrderCols+` WHERE market=? AND status IN ('open','partial') ORDER BY created_at ASC`, market)
	if err != nil {
		return nil, err
	}
	return scanPerpOrders(rows)
}

// ---------- atomic settlement ----------

// CommitPerp atomically applies balance postings, upserts the given positions,
// updates the given perp orders, and (optionally) records a trade — all in one
// transaction so margin, PnL, the ledger, positions and orders never diverge.
// When trade is non-nil its ID is populated on success.
func (s *Store) CommitPerp(txnID string, createdAt int64, postings []Posting, trade *models.Trade, positions []*models.Position, orders []*models.PerpOrder) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := applyPostingsTx(tx, txnID, createdAt, postings); err != nil {
		return err
	}
	for _, p := range positions {
		if _, err := tx.Exec(
			`INSERT INTO positions(user_id,market,side,size,entry_price,margin,leverage,realized_pnl,funding_paid,updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?)
			 ON CONFLICT(user_id,market) DO UPDATE SET
			   side=excluded.side, size=excluded.size, entry_price=excluded.entry_price,
			   margin=excluded.margin, leverage=excluded.leverage,
			   realized_pnl=excluded.realized_pnl, funding_paid=excluded.funding_paid,
			   updated_at=excluded.updated_at`,
			p.UserID, p.Market, p.Side, p.Size.Raw(), p.EntryPrice.Raw(), p.Margin.Raw(),
			p.Leverage, p.RealizedPnL.Raw(), p.FundingPaid.Raw(), p.UpdatedAt); err != nil {
			return err
		}
	}
	for _, o := range orders {
		if _, err := tx.Exec(
			`UPDATE perp_orders SET filled=?, avg_price=?, status=?, updated_at=? WHERE id=?`,
			o.Filled.Raw(), o.AvgPrice.Raw(), o.Status, o.UpdatedAt, o.ID); err != nil {
			return err
		}
	}
	if trade != nil {
		res, err := tx.Exec(
			`INSERT INTO trades(market,price,quantity,quote_qty,taker_side,buy_order_id,sell_order_id,buy_user_id,sell_user_id,created_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`,
			trade.Market, trade.Price.Raw(), trade.Quantity.Raw(), trade.QuoteQty.Raw(), trade.TakerSide,
			trade.BuyOrderID, trade.SellOrderID, trade.BuyUserID, trade.SellUserID, trade.CreatedAt)
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		trade.ID, _ = res.LastInsertId()
		return nil
	}
	return tx.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
