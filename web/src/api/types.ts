// Wire types mirroring the Go backend. All monetary/quantity fields arrive as
// decimal strings to preserve precision; format with utils/format, and only
// convert to number for charting or non-settlement display math.

export interface User {
  id: number;
  email: string;
  kycStatus: "none" | "pending" | "verified";
  role: string;
  createdAt: number;
}

export interface Asset {
  symbol: string;
  name: string;
  kind: "crypto" | "fiat";
  decimals: number;
  network: string;
  withdrawFee: string;
  minWithdraw: string;
  confirmations: number;
}

export interface Market {
  symbol: string;
  base: string;
  quote: string;
  priceTick: string;
  qtyStep: string;
  minNotional: string;
  makerFee: string;
  takerFee: string;
  status: string;
}

export interface Balance {
  asset: string;
  available: string;
  locked: string;
}

export type Side = "buy" | "sell";
export type OrderType = "limit" | "market";
export type OrderStatus = "open" | "partial" | "filled" | "canceled" | "rejected";

export interface Order {
  id: string;
  userId: number;
  market: string;
  side: Side;
  type: OrderType;
  price: string;
  quantity: string;
  filled: string;
  quoteFilled: string;
  feePaid: string;
  status: OrderStatus;
  createdAt: number;
  updatedAt: number;
}

export interface Trade {
  id: number;
  market: string;
  price: string;
  quantity: string;
  quoteQty: string;
  takerSide: Side;
  buyOrderId: string;
  sellOrderId: string;
  createdAt: number;
}

export interface DepthLevel {
  price: string;
  qty: string;
}

export interface Depth {
  market: string;
  bids: DepthLevel[];
  asks: DepthLevel[];
}

export interface Candle {
  time: number;
  open: string;
  high: string;
  low: string;
  close: string;
  volume: string;
}

export interface Ticker {
  market: string;
  last: string;
  open24h: string;
  high24h: string;
  low24h: string;
  volume24h: string;
  quoteVol24h: string;
  change: string;
  changePct: string;
  bestBid: string;
  bestAsk: string;
  updatedAt: number;
}

export interface WalletAddress {
  asset: string;
  address: string;
  network: string;
}

export interface WalletTxn {
  id: string;
  userId: number;
  asset: string;
  type: "deposit" | "withdrawal";
  amount: string;
  fee: string;
  address: string;
  txid: string;
  status: "pending" | "confirmed" | "completed" | "failed";
  confirmations: number;
  createdAt: number;
  updatedAt: number;
}

export interface AuthResponse {
  token: string;
  user: User;
}

// ---------- derivatives (perpetual futures) ----------

export interface PerpMarket {
  symbol: string;
  base: string;
  settle: string;
  indexSymbol: string;
  priceTick: string;
  qtyStep: string;
  minNotional: string;
  makerFee: string;
  takerFee: string;
  maxLeverage: number;
  mmr: string;
  status: string;
}

export type PositionSide = "long" | "short" | "flat";

export interface Position {
  id: number;
  userId: number;
  market: string;
  side: PositionSide;
  size: string;
  entryPrice: string;
  margin: string;
  leverage: number;
  realizedPnl: string;
  fundingPaid: string;
  updatedAt: number;
  // computed against live mark price
  markPrice: string;
  liqPrice: string;
  unrealizedPnl: string;
  notional: string;
  marginRatio: string;
}

export interface PerpOrder {
  id: string;
  userId: number;
  market: string;
  side: Side;
  type: OrderType;
  price: string;
  quantity: string;
  filled: string;
  avgPrice: string;
  leverage: number;
  reduceOnly: boolean;
  status: OrderStatus;
  createdAt: number;
  updatedAt: number;
}

export interface FundingInfo {
  market: string;
  rate: string;
  indexPrice: string;
  markPrice: string;
  intervalSec: number;
  nextFundingTime: number;
}
