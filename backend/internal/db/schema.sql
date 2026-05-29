-- Exchange schema. All monetary amounts are stored as INTEGER values scaled by
-- 1e8 (see internal/num). Times are unix seconds unless noted.

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    kyc_status    TEXT NOT NULL DEFAULT 'none',
    role          TEXT NOT NULL DEFAULT 'user',
    created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS assets (
    symbol        TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL,            -- crypto | fiat
    decimals      INTEGER NOT NULL,
    network       TEXT NOT NULL DEFAULT '',
    withdraw_fee  INTEGER NOT NULL DEFAULT 0,
    min_withdraw  INTEGER NOT NULL DEFAULT 0,
    confirmations INTEGER NOT NULL DEFAULT 2
);

CREATE TABLE IF NOT EXISTS markets (
    symbol       TEXT PRIMARY KEY,          -- e.g. BTC-USDT
    base         TEXT NOT NULL,
    quote        TEXT NOT NULL,
    price_tick   INTEGER NOT NULL,
    qty_step     INTEGER NOT NULL,
    min_notional INTEGER NOT NULL,
    maker_fee    INTEGER NOT NULL,
    taker_fee    INTEGER NOT NULL,
    status       TEXT NOT NULL DEFAULT 'trading'
);

CREATE TABLE IF NOT EXISTS balances (
    user_id   INTEGER NOT NULL,
    asset     TEXT NOT NULL,
    available INTEGER NOT NULL DEFAULT 0,
    locked    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, asset)
);

-- Append-only journal of every balance mutation for audit / double-entry.
CREATE TABLE IF NOT EXISTS ledger_entries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    txn_id          TEXT NOT NULL,
    user_id         INTEGER NOT NULL,
    asset           TEXT NOT NULL,
    delta_available INTEGER NOT NULL,
    delta_locked    INTEGER NOT NULL,
    reason          TEXT NOT NULL,
    ref             TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ledger_user ON ledger_entries(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ledger_txn  ON ledger_entries(txn_id);

CREATE TABLE IF NOT EXISTS orders (
    id           TEXT PRIMARY KEY,
    user_id      INTEGER NOT NULL,
    market       TEXT NOT NULL,
    side         TEXT NOT NULL,
    type         TEXT NOT NULL,
    price        INTEGER NOT NULL DEFAULT 0,
    quantity     INTEGER NOT NULL,
    filled       INTEGER NOT NULL DEFAULT 0,
    quote_filled INTEGER NOT NULL DEFAULT 0,
    fee_paid     INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_orders_user   ON orders(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_market ON orders(market, status);

CREATE TABLE IF NOT EXISTS trades (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    market        TEXT NOT NULL,
    price         INTEGER NOT NULL,
    quantity      INTEGER NOT NULL,
    quote_qty     INTEGER NOT NULL,
    taker_side    TEXT NOT NULL,
    buy_order_id  TEXT NOT NULL,
    sell_order_id TEXT NOT NULL,
    buy_user_id   INTEGER NOT NULL,
    sell_user_id  INTEGER NOT NULL,
    created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_trades_market ON trades(market, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_trades_buyer  ON trades(buy_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_trades_seller ON trades(sell_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS wallet_addresses (
    user_id INTEGER NOT NULL,
    asset   TEXT NOT NULL,
    address TEXT NOT NULL,
    network TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, asset)
);

CREATE TABLE IF NOT EXISTS wallet_txns (
    id            TEXT PRIMARY KEY,
    user_id       INTEGER NOT NULL,
    asset         TEXT NOT NULL,
    type          TEXT NOT NULL,
    amount        INTEGER NOT NULL,
    fee           INTEGER NOT NULL DEFAULT 0,
    address       TEXT NOT NULL DEFAULT '',
    txid          TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,
    confirmations INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_wtxn_user ON wallet_txns(user_id, created_at DESC);
