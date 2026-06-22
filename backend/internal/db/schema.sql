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

-- ===================== Derivatives (perpetual futures) =====================

CREATE TABLE IF NOT EXISTS perp_markets (
    symbol       TEXT PRIMARY KEY,          -- e.g. BTC-PERP
    base         TEXT NOT NULL,
    settle       TEXT NOT NULL,             -- margin/PnL currency (USDT)
    index_symbol TEXT NOT NULL DEFAULT '',  -- spot market used as funding index
    price_tick   INTEGER NOT NULL,
    qty_step     INTEGER NOT NULL,
    min_notional INTEGER NOT NULL,
    maker_fee    INTEGER NOT NULL,
    taker_fee    INTEGER NOT NULL,
    max_leverage INTEGER NOT NULL DEFAULT 1,
    mmr          INTEGER NOT NULL,          -- maintenance margin rate (scaled)
    status       TEXT NOT NULL DEFAULT 'trading'
);

-- One netted (one-way) position per (user, market). size=0 means flat.
CREATE TABLE IF NOT EXISTS positions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL,
    market       TEXT NOT NULL,
    side         TEXT NOT NULL DEFAULT 'flat',
    size         INTEGER NOT NULL DEFAULT 0,
    entry_price  INTEGER NOT NULL DEFAULT 0,
    margin       INTEGER NOT NULL DEFAULT 0,
    leverage     INTEGER NOT NULL DEFAULT 1,
    realized_pnl INTEGER NOT NULL DEFAULT 0,
    funding_paid INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL,
    UNIQUE(user_id, market)
);
CREATE INDEX IF NOT EXISTS idx_positions_market ON positions(market);
CREATE INDEX IF NOT EXISTS idx_positions_user   ON positions(user_id);

CREATE TABLE IF NOT EXISTS perp_orders (
    id          TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL,
    market      TEXT NOT NULL,
    side        TEXT NOT NULL,
    type        TEXT NOT NULL,
    price       INTEGER NOT NULL DEFAULT 0,
    quantity    INTEGER NOT NULL,
    filled      INTEGER NOT NULL DEFAULT 0,
    avg_price   INTEGER NOT NULL DEFAULT 0,
    leverage    INTEGER NOT NULL DEFAULT 1,
    reduce_only INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_porders_user   ON perp_orders(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_porders_market ON perp_orders(market, status);

-- ===================== Earn (savings / staking) =====================

CREATE TABLE IF NOT EXISTS earn_products (
    id         TEXT PRIMARY KEY,          -- e.g. BTC-FLEX, USDT-30D
    asset      TEXT NOT NULL,
    kind       TEXT NOT NULL,             -- flexible | fixed
    apr        INTEGER NOT NULL,          -- annualized fraction, scaled 1e8
    term_days  INTEGER NOT NULL DEFAULT 0,
    min_amount INTEGER NOT NULL DEFAULT 0,
    max_amount INTEGER NOT NULL DEFAULT 0, -- 0 = uncapped
    status     TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE IF NOT EXISTS earn_positions (
    id              TEXT PRIMARY KEY,
    user_id         INTEGER NOT NULL,
    product_id      TEXT NOT NULL,
    asset           TEXT NOT NULL,
    kind            TEXT NOT NULL,
    principal       INTEGER NOT NULL,
    apr             INTEGER NOT NULL,
    accrued_total   INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'active',
    start_at        INTEGER NOT NULL,
    maturity_at     INTEGER NOT NULL DEFAULT 0,
    last_accrual_at INTEGER NOT NULL,
    redeemed_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_earn_pos_user   ON earn_positions(user_id, start_at DESC);
CREATE INDEX IF NOT EXISTS idx_earn_pos_status ON earn_positions(status);
