package store

import "database/sql"

// Hot-path statements are prepared once, when the Store is created. In a short
// write transaction SQLite's parse/plan step costs about as much as the work
// itself, so reusing the compiled form raises matching throughput. Inside a
// transaction they are bound with tx.Stmt, which reuses the prepared form on
// the transaction's connection. (They cannot be prepared lazily: with a
// single-connection pool, preparing on the DB while a transaction holds the
// connection would deadlock.)
const (
	sqlUpsertBalance = `INSERT INTO balances(user_id,asset,available,locked) VALUES(?,?,?,?)
		ON CONFLICT(user_id,asset) DO UPDATE SET
		  available=available+excluded.available, locked=locked+excluded.locked
		RETURNING available,locked`
	sqlInsertLedger = `INSERT INTO ledger_entries(txn_id,user_id,asset,delta_available,delta_locked,reason,ref,created_at)
		VALUES(?,?,?,?,?,?,?,?)`
	sqlInsertTrade = `INSERT INTO trades(market,price,quantity,quote_qty,taker_side,buy_order_id,sell_order_id,buy_user_id,sell_user_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`
	sqlInsertOrder = `INSERT INTO orders(id,user_id,market,side,type,price,quantity,filled,quote_filled,fee_paid,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`
	sqlUpdateOrder     = `UPDATE orders SET filled=?, quote_filled=?, fee_paid=?, status=?, updated_at=? WHERE id=?`
	sqlInsertPerpOrder = `INSERT INTO perp_orders(id,user_id,market,side,type,price,quantity,filled,avg_price,leverage,reduce_only,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	sqlUpdatePerpOrder = `UPDATE perp_orders SET filled=?, avg_price=?, status=?, updated_at=? WHERE id=?`
	sqlUpsertPosition  = `INSERT INTO positions(user_id,market,side,size,entry_price,margin,leverage,realized_pnl,funding_paid,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id,market) DO UPDATE SET
		  side=excluded.side, size=excluded.size, entry_price=excluded.entry_price,
		  margin=excluded.margin, leverage=excluded.leverage,
		  realized_pnl=excluded.realized_pnl, funding_paid=excluded.funding_paid,
		  updated_at=excluded.updated_at`
	sqlListBalances = `SELECT asset,available,locked FROM balances WHERE user_id=? ORDER BY asset`
)

var hotStatements = []string{
	sqlUpsertBalance, sqlInsertLedger, sqlInsertTrade, sqlInsertOrder, sqlUpdateOrder,
	sqlInsertPerpOrder, sqlUpdatePerpOrder, sqlUpsertPosition, sqlListBalances,
}

// prepareHot prepares hotStatements. A statement that fails to prepare is
// simply left out; exec/query fall back to the unprepared path for it.
func (s *Store) prepareHot() {
	s.stmts = make(map[string]*sql.Stmt, len(hotStatements))
	for _, q := range hotStatements {
		if st, err := s.db.Prepare(q); err == nil {
			s.stmts[q] = st
		}
	}
}

// exec runs query inside tx (or on the DB when tx is nil), using its prepared
// form when one exists. s.stmts is read-only after New, so no lock is needed.
func (s *Store) exec(tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	st := s.stmts[query]
	switch {
	case st != nil && tx != nil:
		return tx.Stmt(st).Exec(args...)
	case st != nil:
		return st.Exec(args...)
	case tx != nil:
		return tx.Exec(query, args...)
	}
	return s.db.Exec(query, args...)
}

func (s *Store) queryRow(tx *sql.Tx, query string, args ...any) *sql.Row {
	st := s.stmts[query]
	switch {
	case st != nil && tx != nil:
		return tx.Stmt(st).QueryRow(args...)
	case st != nil:
		return st.QueryRow(args...)
	case tx != nil:
		return tx.QueryRow(query, args...)
	}
	return s.db.QueryRow(query, args...)
}

func (s *Store) query(query string, args ...any) (*sql.Rows, error) {
	if st := s.stmts[query]; st != nil {
		return st.Query(args...)
	}
	return s.db.Query(query, args...)
}
