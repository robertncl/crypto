// Package store is the data-access layer over SQLite. It exposes typed CRUD
// helpers plus ApplyPostings, the single atomic primitive through which every
// balance mutation flows so that funds and the audit ledger never diverge.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
)

// ExchangeUserID is the reserved account that collects trading fees. It is not a
// row in users; balances/ledger simply reference this id.
const ExchangeUserID int64 = 0

var (
	// ErrInsufficientFunds is returned when a posting would drive a balance
	// negative (available or locked).
	ErrInsufficientFunds = errors.New("insufficient funds")
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("not found")
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) DB() *sql.DB { return s.db }

// ---------- Users ----------

func (s *Store) CreateUser(email, hash, role string, createdAt int64) (*models.User, error) {
	res, err := s.db.Exec(
		`INSERT INTO users(email,password_hash,kyc_status,role,created_at) VALUES(?,?,?,?,?)`,
		email, hash, "none", role, createdAt)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &models.User{ID: id, Email: email, PasswordHash: hash, KYCStatus: "none", Role: role, CreatedAt: createdAt}, nil
}

func (s *Store) GetUserByEmail(email string) (*models.User, error) {
	row := s.db.QueryRow(`SELECT id,email,password_hash,kyc_status,role,created_at FROM users WHERE email=?`, email)
	return scanUser(row)
}

func (s *Store) GetUserByID(id int64) (*models.User, error) {
	row := s.db.QueryRow(`SELECT id,email,password_hash,kyc_status,role,created_at FROM users WHERE id=?`, id)
	return scanUser(row)
}

func (s *Store) SetKYCStatus(userID int64, status string) error {
	_, err := s.db.Exec(`UPDATE users SET kyc_status=? WHERE id=?`, status, userID)
	return err
}

func scanUser(row *sql.Row) (*models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.KYCStatus, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ---------- Assets & markets ----------

func (s *Store) ListAssets() ([]models.Asset, error) {
	rows, err := s.db.Query(`SELECT symbol,name,kind,decimals,network,withdraw_fee,min_withdraw,confirmations FROM assets ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Asset
	for rows.Next() {
		var a models.Asset
		var wf, mw int64
		if err := rows.Scan(&a.Symbol, &a.Name, &a.Kind, &a.Decimals, &a.Network, &wf, &mw, &a.Confirmations); err != nil {
			return nil, err
		}
		a.WithdrawFee, a.MinWithdraw = num.FromRaw(wf), num.FromRaw(mw)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAsset(symbol string) (*models.Asset, error) {
	row := s.db.QueryRow(`SELECT symbol,name,kind,decimals,network,withdraw_fee,min_withdraw,confirmations FROM assets WHERE symbol=?`, symbol)
	var a models.Asset
	var wf, mw int64
	err := row.Scan(&a.Symbol, &a.Name, &a.Kind, &a.Decimals, &a.Network, &wf, &mw, &a.Confirmations)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.WithdrawFee, a.MinWithdraw = num.FromRaw(wf), num.FromRaw(mw)
	return &a, nil
}

func (s *Store) ListMarkets() ([]models.Market, error) {
	rows, err := s.db.Query(`SELECT symbol,base,quote,price_tick,qty_step,min_notional,maker_fee,taker_fee,status FROM markets ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Market
	for rows.Next() {
		m, err := scanMarketRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *Store) GetMarket(symbol string) (*models.Market, error) {
	row := s.db.QueryRow(`SELECT symbol,base,quote,price_tick,qty_step,min_notional,maker_fee,taker_fee,status FROM markets WHERE symbol=?`, symbol)
	var m models.Market
	var pt, qs, mn, mf, tf int64
	err := row.Scan(&m.Symbol, &m.Base, &m.Quote, &pt, &qs, &mn, &mf, &tf, &m.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.PriceTick, m.QtyStep, m.MinNotional = num.FromRaw(pt), num.FromRaw(qs), num.FromRaw(mn)
	m.MakerFee, m.TakerFee = num.FromRaw(mf), num.FromRaw(tf)
	return &m, nil
}

func scanMarketRows(rows *sql.Rows) (*models.Market, error) {
	var m models.Market
	var pt, qs, mn, mf, tf int64
	if err := rows.Scan(&m.Symbol, &m.Base, &m.Quote, &pt, &qs, &mn, &mf, &tf, &m.Status); err != nil {
		return nil, err
	}
	m.PriceTick, m.QtyStep, m.MinNotional = num.FromRaw(pt), num.FromRaw(qs), num.FromRaw(mn)
	m.MakerFee, m.TakerFee = num.FromRaw(mf), num.FromRaw(tf)
	return &m, nil
}

// ---------- Balances ----------

func (s *Store) ListBalances(userID int64) ([]models.Balance, error) {
	rows, err := s.db.Query(`SELECT asset,available,locked FROM balances WHERE user_id=? ORDER BY asset`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Balance
	for rows.Next() {
		var b models.Balance
		var av, lk int64
		if err := rows.Scan(&b.Asset, &av, &lk); err != nil {
			return nil, err
		}
		b.Available, b.Locked = num.FromRaw(av), num.FromRaw(lk)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBalance(userID int64, asset string) (models.Balance, error) {
	row := s.db.QueryRow(`SELECT available,locked FROM balances WHERE user_id=? AND asset=?`, userID, asset)
	var av, lk int64
	err := row.Scan(&av, &lk)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Balance{Asset: asset, Available: num.Zero, Locked: num.Zero}, nil
	}
	if err != nil {
		return models.Balance{}, err
	}
	return models.Balance{Asset: asset, Available: num.FromRaw(av), Locked: num.FromRaw(lk)}, nil
}

// Posting is a single signed change to one (user, asset) balance. Positive
// deltas credit, negative deltas debit.
type Posting struct {
	UserID         int64
	Asset          string
	DeltaAvailable num.Dec
	DeltaLocked    num.Dec
	Reason         string
	Ref            string
}

// ApplyPostings applies a set of balance changes atomically and writes a ledger
// entry for each. If any resulting available or locked balance would be
// negative, the whole transaction rolls back with ErrInsufficientFunds. This is
// the only sanctioned way to mutate balances.
func (s *Store) ApplyPostings(txnID string, createdAt int64, postings []Posting) error {
	if len(postings) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, p := range postings {
		// Upsert the balance row then apply the delta.
		if _, err := tx.Exec(
			`INSERT INTO balances(user_id,asset,available,locked) VALUES(?,?,0,0)
			 ON CONFLICT(user_id,asset) DO NOTHING`, p.UserID, p.Asset); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`UPDATE balances SET available=available+?, locked=locked+? WHERE user_id=? AND asset=?`,
			p.DeltaAvailable.Raw(), p.DeltaLocked.Raw(), p.UserID, p.Asset); err != nil {
			return err
		}
		// Validate non-negativity after the change.
		var av, lk int64
		if err := tx.QueryRow(`SELECT available,locked FROM balances WHERE user_id=? AND asset=?`,
			p.UserID, p.Asset).Scan(&av, &lk); err != nil {
			return err
		}
		if av < 0 || lk < 0 {
			return fmt.Errorf("%w: user=%d asset=%s available=%d locked=%d",
				ErrInsufficientFunds, p.UserID, p.Asset, av, lk)
		}
		if _, err := tx.Exec(
			`INSERT INTO ledger_entries(txn_id,user_id,asset,delta_available,delta_locked,reason,ref,created_at)
			 VALUES(?,?,?,?,?,?,?,?)`,
			txnID, p.UserID, p.Asset, p.DeltaAvailable.Raw(), p.DeltaLocked.Raw(), p.Reason, p.Ref, createdAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- Orders ----------

func (s *Store) InsertOrder(o *models.Order) error {
	_, err := s.db.Exec(
		`INSERT INTO orders(id,user_id,market,side,type,price,quantity,filled,quote_filled,fee_paid,status,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.UserID, o.Market, o.Side, o.Type, o.Price.Raw(), o.Quantity.Raw(),
		o.Filled.Raw(), o.QuoteFilled.Raw(), o.FeePaid.Raw(), o.Status, o.CreatedAt, o.UpdatedAt)
	return err
}

func (s *Store) UpdateOrder(o *models.Order) error {
	_, err := s.db.Exec(
		`UPDATE orders SET filled=?, quote_filled=?, fee_paid=?, status=?, updated_at=? WHERE id=?`,
		o.Filled.Raw(), o.QuoteFilled.Raw(), o.FeePaid.Raw(), o.Status, o.UpdatedAt, o.ID)
	return err
}

func (s *Store) GetOrder(id string) (*models.Order, error) {
	row := s.db.QueryRow(orderCols+` WHERE id=?`, id)
	return scanOrderRow(row)
}

const orderCols = `SELECT id,user_id,market,side,type,price,quantity,filled,quote_filled,fee_paid,status,created_at,updated_at FROM orders`

func scanOrderRow(row *sql.Row) (*models.Order, error) {
	var o models.Order
	var price, qty, filled, qfilled, fee int64
	err := row.Scan(&o.ID, &o.UserID, &o.Market, &o.Side, &o.Type, &price, &qty, &filled, &qfilled, &fee,
		&o.Status, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	o.Price, o.Quantity, o.Filled = num.FromRaw(price), num.FromRaw(qty), num.FromRaw(filled)
	o.QuoteFilled, o.FeePaid = num.FromRaw(qfilled), num.FromRaw(fee)
	return &o, nil
}

func scanOrders(rows *sql.Rows) ([]models.Order, error) {
	defer rows.Close()
	var out []models.Order
	for rows.Next() {
		var o models.Order
		var price, qty, filled, qfilled, fee int64
		if err := rows.Scan(&o.ID, &o.UserID, &o.Market, &o.Side, &o.Type, &price, &qty, &filled, &qfilled, &fee,
			&o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		o.Price, o.Quantity, o.Filled = num.FromRaw(price), num.FromRaw(qty), num.FromRaw(filled)
		o.QuoteFilled, o.FeePaid = num.FromRaw(qfilled), num.FromRaw(fee)
		out = append(out, o)
	}
	return out, rows.Err()
}

// ListOpenOrders returns a user's currently working orders, optionally filtered
// by market (empty = all markets).
func (s *Store) ListOpenOrders(userID int64, market string) ([]models.Order, error) {
	q := orderCols + ` WHERE user_id=? AND status IN ('open','partial')`
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
	return scanOrders(rows)
}

// ListOrderHistory returns terminal + working orders for a user, newest first.
func (s *Store) ListOrderHistory(userID int64, market string, limit int) ([]models.Order, error) {
	q := orderCols + ` WHERE user_id=?`
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
	return scanOrders(rows)
}

// ListWorkingOrdersByMarket loads all open/partial orders for a market so the
// engine can rebuild its in-memory book on startup, oldest first (time priority).
func (s *Store) ListWorkingOrdersByMarket(market string) ([]models.Order, error) {
	rows, err := s.db.Query(orderCols+` WHERE market=? AND status IN ('open','partial') ORDER BY created_at ASC`, market)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

// ---------- Trades ----------

func (s *Store) InsertTrade(t *models.Trade) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO trades(market,price,quantity,quote_qty,taker_side,buy_order_id,sell_order_id,buy_user_id,sell_user_id,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		t.Market, t.Price.Raw(), t.Quantity.Raw(), t.QuoteQty.Raw(), t.TakerSide,
		t.BuyOrderID, t.SellOrderID, t.BuyUserID, t.SellUserID, t.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListTradesByMarket(market string, limit int) ([]models.Trade, error) {
	rows, err := s.db.Query(
		`SELECT id,market,price,quantity,quote_qty,taker_side,buy_order_id,sell_order_id,buy_user_id,sell_user_id,created_at
		 FROM trades WHERE market=? ORDER BY id DESC LIMIT ?`, market, limit)
	if err != nil {
		return nil, err
	}
	return scanTrades(rows)
}

// ListTradesByUser returns trades where the user was buyer or seller.
func (s *Store) ListTradesByUser(userID int64, market string, limit int) ([]models.Trade, error) {
	q := `SELECT id,market,price,quantity,quote_qty,taker_side,buy_order_id,sell_order_id,buy_user_id,sell_user_id,created_at
	      FROM trades WHERE (buy_user_id=? OR sell_user_id=?)`
	args := []any{userID, userID}
	if market != "" {
		q += ` AND market=?`
		args = append(args, market)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return scanTrades(rows)
}

func scanTrades(rows *sql.Rows) ([]models.Trade, error) {
	defer rows.Close()
	var out []models.Trade
	for rows.Next() {
		var t models.Trade
		var price, qty, qq int64
		if err := rows.Scan(&t.ID, &t.Market, &price, &qty, &qq, &t.TakerSide,
			&t.BuyOrderID, &t.SellOrderID, &t.BuyUserID, &t.SellUserID, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Price, t.Quantity, t.QuoteQty = num.FromRaw(price), num.FromRaw(qty), num.FromRaw(qq)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Candle aggregates trades into OHLCV buckets of intervalSec seconds, returning
// up to limit most-recent buckets in ascending time order.
func (s *Store) Candles(market string, intervalSec int64, limit int) ([]models.Candle, error) {
	// Group trades by floor(created_at/interval)*interval. SQLite has no window
	// frame for first/last by time cheaply, so we approximate open/close using
	// MIN/MAX id within the bucket via a correlated subquery would be costly;
	// instead we fetch raw trades for the recent window and fold in Go.
	windowStart := int64(0)
	rows, err := s.db.Query(
		`SELECT price,quantity,created_at FROM trades WHERE market=? AND created_at>=? ORDER BY id ASC`,
		market, windowStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type agg struct {
		o, h, l, c, v num.Dec
		set           bool
	}
	buckets := map[int64]*agg{}
	var order []int64
	for rows.Next() {
		var p, q, ts int64
		if err := rows.Scan(&p, &q, &ts); err != nil {
			return nil, err
		}
		price, qty := num.FromRaw(p), num.FromRaw(q)
		bt := (ts / intervalSec) * intervalSec
		a := buckets[bt]
		if a == nil {
			a = &agg{o: price, h: price, l: price, c: price}
			buckets[bt] = a
			order = append(order, bt)
		}
		a.h = num.Max(a.h, price)
		a.l = num.Min(a.l, price)
		a.c = price
		a.v = a.v.Add(qty)
		a.set = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]models.Candle, 0, len(order))
	for _, bt := range order {
		a := buckets[bt]
		out = append(out, models.Candle{Time: bt, Open: a.o, High: a.h, Low: a.l, Close: a.c, Volume: a.v})
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// ---------- Wallet ----------

func (s *Store) GetOrCreateAddress(userID int64, asset, network string, gen func() string) (models.WalletAddress, error) {
	row := s.db.QueryRow(`SELECT address,network FROM wallet_addresses WHERE user_id=? AND asset=?`, userID, asset)
	var addr, net string
	err := row.Scan(&addr, &net)
	if err == nil {
		return models.WalletAddress{Asset: asset, Address: addr, Network: net}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return models.WalletAddress{}, err
	}
	addr = gen()
	if _, err := s.db.Exec(`INSERT INTO wallet_addresses(user_id,asset,address,network) VALUES(?,?,?,?)`,
		userID, asset, addr, network); err != nil {
		return models.WalletAddress{}, err
	}
	return models.WalletAddress{Asset: asset, Address: addr, Network: network}, nil
}

func (s *Store) InsertTxn(t *models.WalletTxn) error {
	_, err := s.db.Exec(
		`INSERT INTO wallet_txns(id,user_id,asset,type,amount,fee,address,txid,status,confirmations,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.UserID, t.Asset, t.Type, t.Amount.Raw(), t.Fee.Raw(), t.Address, t.TxID,
		t.Status, t.Confirmations, t.CreatedAt, t.UpdatedAt)
	return err
}

func (s *Store) UpdateTxn(t *models.WalletTxn) error {
	_, err := s.db.Exec(
		`UPDATE wallet_txns SET status=?, confirmations=?, txid=?, updated_at=? WHERE id=?`,
		t.Status, t.Confirmations, t.TxID, t.UpdatedAt, t.ID)
	return err
}

func (s *Store) ListTxns(userID int64, limit int) ([]models.WalletTxn, error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,asset,type,amount,fee,address,txid,status,confirmations,created_at,updated_at
		 FROM wallet_txns WHERE user_id=? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.WalletTxn
	for rows.Next() {
		var t models.WalletTxn
		var amt, fee int64
		if err := rows.Scan(&t.ID, &t.UserID, &t.Asset, &t.Type, &amt, &fee, &t.Address, &t.TxID,
			&t.Status, &t.Confirmations, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Amount, t.Fee = num.FromRaw(amt), num.FromRaw(fee)
		out = append(out, t)
	}
	return out, rows.Err()
}
