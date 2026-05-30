// Package db opens the SQLite database, applies the schema, and seeds reference
// data (assets and markets). It uses the pure-Go modernc.org/sqlite driver so
// the project builds without CGO/gcc.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"net/url"
	"strings"

	"cryptoex/internal/num"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Open opens (creating if needed) the SQLite database at path with pragmas
// tuned for a small concurrent server, applies the schema, and seeds reference
// data. A single shared *sql.DB is returned; SQLite serializes writes so this
// is safe for concurrent goroutines.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)",
		url.PathEscape(path))
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc serializes through a single underlying connection most safely when
	// we cap the pool; WAL still allows concurrent readers.
	conn.SetMaxOpenConns(1)
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := conn.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := seed(conn); err != nil {
		return nil, fmt.Errorf("seed: %w", err)
	}
	return conn, nil
}

// seedAsset and seedMarket describe the reference data inserted on first run.
type seedAsset struct {
	symbol, name, kind, network string
	decimals, confirmations     int
	withdrawFee, minWithdraw    string
}

type seedMarket struct {
	base, quote                      string
	priceTick, qtyStep, minNotional  string
	makerFee, takerFee               string
}

func seed(conn *sql.DB) error {
	assets := []seedAsset{
		{"USDT", "Tether", "fiat", "TRC20", 8, 1, "1", "10"},
		{"BTC", "Bitcoin", "crypto", "Bitcoin", 8, 2, "0.0002", "0.0005"},
		{"ETH", "Ethereum", "crypto", "ERC20", 8, 2, "0.003", "0.01"},
		{"SOL", "Solana", "crypto", "Solana", 8, 1, "0.01", "0.05"},
		{"BNB", "BNB", "crypto", "BEP20", 8, 1, "0.001", "0.01"},
	}
	for _, a := range assets {
		if _, err := conn.Exec(
			`INSERT OR IGNORE INTO assets(symbol,name,kind,decimals,network,withdraw_fee,min_withdraw,confirmations)
			 VALUES(?,?,?,?,?,?,?,?)`,
			a.symbol, a.name, a.kind, a.decimals, a.network,
			num.MustParse(a.withdrawFee).Raw(), num.MustParse(a.minWithdraw).Raw(), a.confirmations,
		); err != nil {
			return err
		}
	}

	markets := []seedMarket{
		{"BTC", "USDT", "0.01", "0.00001", "5", "0.001", "0.001"},
		{"ETH", "USDT", "0.01", "0.0001", "5", "0.001", "0.001"},
		{"SOL", "USDT", "0.001", "0.01", "5", "0.001", "0.001"},
		{"BNB", "USDT", "0.01", "0.001", "5", "0.001", "0.001"},
	}
	for _, m := range markets {
		symbol := m.base + "-" + m.quote
		if _, err := conn.Exec(
			`INSERT OR IGNORE INTO markets(symbol,base,quote,price_tick,qty_step,min_notional,maker_fee,taker_fee,status)
			 VALUES(?,?,?,?,?,?,?,?,'trading')`,
			symbol, m.base, m.quote,
			num.MustParse(m.priceTick).Raw(), num.MustParse(m.qtyStep).Raw(),
			num.MustParse(m.minNotional).Raw(), num.MustParse(m.makerFee).Raw(),
			num.MustParse(m.takerFee).Raw(),
		); err != nil {
			return err
		}
	}

	// Perpetual futures markets (linear, USDT-margined), indexed to the matching
	// spot pair for funding.
	perps := []seedPerp{
		{"BTC", "0.1", "0.001", "5", "0.0002", "0.0006", 100, "0.005"},
		{"ETH", "0.01", "0.01", "5", "0.0002", "0.0006", 100, "0.005"},
		{"SOL", "0.001", "0.1", "5", "0.0002", "0.0006", 50, "0.01"},
	}
	for _, p := range perps {
		symbol := p.base + "-PERP"
		if _, err := conn.Exec(
			`INSERT OR IGNORE INTO perp_markets(symbol,base,settle,index_symbol,price_tick,qty_step,min_notional,maker_fee,taker_fee,max_leverage,mmr,status)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,'trading')`,
			symbol, p.base, "USDT", p.base+"-USDT",
			num.MustParse(p.priceTick).Raw(), num.MustParse(p.qtyStep).Raw(),
			num.MustParse(p.minNotional).Raw(), num.MustParse(p.makerFee).Raw(),
			num.MustParse(p.takerFee).Raw(), p.maxLev, num.MustParse(p.mmr).Raw(),
		); err != nil {
			return err
		}
	}
	return nil
}

type seedPerp struct {
	base                            string
	priceTick, qtyStep, minNotional string
	makerFee, takerFee              string
	maxLev                          int
	mmr                             string
}

// InClause builds a "(?,?,...)" placeholder list and the matching args slice for
// a set of string ids; a small helper used by the store.
func InClause(ids []string) (string, []any) {
	if len(ids) == 0 {
		return "(NULL)", nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	return "(" + strings.Join(ph, ",") + ")", args
}
