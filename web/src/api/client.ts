// Thin typed wrapper over fetch for the REST API. The bearer token is held in
// module scope and injected on every request; 401s surface as ApiError so the
// auth layer can react (e.g. log the user out).

import type {
  AuthResponse, Asset, Balance, Candle, Depth, Market, Order, Side,
  OrderType, Ticker, Trade, User, WalletAddress, WalletTxn,
  PerpMarket, Position, PerpOrder, FundingInfo, EarnProduct, EarnPosition,
} from "./types";

let token: string | null = null;

export function setToken(t: string | null) {
  token = t;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init?.body) headers.set("Content-Type", "application/json");

  const res = await fetch(`/api${path}`, { ...init, headers });
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? `request failed (${res.status})`);
  }
  return data as T;
}

export const api = {
  // auth
  register: (email: string, password: string) =>
    request<AuthResponse>("/auth/register", { method: "POST", body: JSON.stringify({ email, password }) }),
  login: (email: string, password: string) =>
    request<AuthResponse>("/auth/login", { method: "POST", body: JSON.stringify({ email, password }) }),
  me: () => request<User>("/me"),
  verifyKyc: () => request<User>("/kyc/verify", { method: "POST" }),

  // reference + market data
  assets: () => request<Asset[]>("/assets"),
  markets: () => request<Market[]>("/markets"),
  market: (symbol: string) => request<Market>(`/markets/${symbol}`),
  tickers: () => request<Ticker[]>("/tickers"),
  depth: (symbol: string, limit = 50) => request<Depth>(`/markets/${symbol}/depth?limit=${limit}`),
  marketTrades: (symbol: string, limit = 50) => request<Trade[]>(`/markets/${symbol}/trades?limit=${limit}`),
  candles: (symbol: string, interval: string, limit = 300) =>
    request<Candle[]>(`/markets/${symbol}/candles?interval=${interval}&limit=${limit}`),

  // account
  balances: () => request<Balance[]>("/account/balances"),
  openOrders: (market?: string) => request<Order[]>(`/orders${market ? `?market=${market}` : ""}`),
  orderHistory: (market?: string) => request<Order[]>(`/orders/history${market ? `?market=${market}` : ""}`),
  myTrades: (market?: string) => request<Trade[]>(`/trades${market ? `?market=${market}` : ""}`),
  placeOrder: (body: { market: string; side: Side; type: OrderType; price?: string; quantity: string }) =>
    request<Order>("/orders", { method: "POST", body: JSON.stringify({ price: "0", ...body }) }),
  cancelOrder: (id: string) => request<Order>(`/orders/${id}`, { method: "DELETE" }),

  // derivatives
  perpMarkets: () => request<PerpMarket[]>("/perp/markets"),
  perpMarket: (symbol: string) => request<PerpMarket>(`/perp/markets/${symbol}`),
  perpDepth: (symbol: string, limit = 50) => request<Depth>(`/perp/markets/${symbol}/depth?limit=${limit}`),
  funding: (symbol: string) => request<FundingInfo>(`/perp/markets/${symbol}/funding`),
  positions: () => request<Position[]>("/perp/positions"),
  openPerpOrders: (market?: string) => request<PerpOrder[]>(`/perp/orders${market ? `?market=${market}` : ""}`),
  perpOrderHistory: (market?: string) => request<PerpOrder[]>(`/perp/orders/history${market ? `?market=${market}` : ""}`),
  placePerpOrder: (body: { market: string; side: Side; type: OrderType; price?: string; quantity: string; leverage: number; reduceOnly?: boolean }) =>
    request<PerpOrder>("/perp/orders", { method: "POST", body: JSON.stringify({ price: "0", reduceOnly: false, ...body }) }),
  cancelPerpOrder: (id: string) => request<PerpOrder>(`/perp/orders/${id}`, { method: "DELETE" }),
  closePosition: (symbol: string) => request<PerpOrder>(`/perp/positions/${symbol}/close`, { method: "POST" }),

  // wallet
  walletAddress: (asset: string) => request<WalletAddress>(`/wallet/address?asset=${asset}`),
  deposit: (asset: string, amount: string) =>
    request<WalletTxn>("/wallet/deposit", { method: "POST", body: JSON.stringify({ asset, amount }) }),
  withdraw: (asset: string, address: string, amount: string) =>
    request<WalletTxn>("/wallet/withdraw", { method: "POST", body: JSON.stringify({ asset, address, amount }) }),
  walletTxns: () => request<WalletTxn[]>("/wallet/transactions"),

  // earn
  earnProducts: () => request<EarnProduct[]>("/earn/products"),
  earnPositions: () => request<EarnPosition[]>("/earn/positions"),
  earnSubscribe: (productId: string, amount: string) =>
    request<EarnPosition>("/earn/subscribe", { method: "POST", body: JSON.stringify({ productId, amount }) }),
  earnRedeem: (id: string) => request<EarnPosition>(`/earn/positions/${id}/redeem`, { method: "POST" }),
};
