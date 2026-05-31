import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { lazy } from "react";
import { Layout } from "./components/Layout";

// Route-level code splitting: each page (and its deps — notably the Trade/Futures
// pages' lightweight-charts) loads on demand instead of in the initial bundle.
// The <Suspense> boundary lives in Layout, around the <Outlet>.
const Markets = lazy(() => import("./pages/Markets").then((m) => ({ default: m.Markets })));
const Trade = lazy(() => import("./pages/Trade").then((m) => ({ default: m.Trade })));
const Futures = lazy(() => import("./pages/Futures").then((m) => ({ default: m.Futures })));
const Wallet = lazy(() => import("./pages/Wallet").then((m) => ({ default: m.Wallet })));
const Login = lazy(() => import("./pages/Login").then((m) => ({ default: m.Login })));
const Register = lazy(() => import("./pages/Register").then((m) => ({ default: m.Register })));

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<Navigate to="/markets" replace />} />
          <Route path="/markets" element={<Markets />} />
          <Route path="/trade" element={<Navigate to="/trade/BTC-USDT" replace />} />
          <Route path="/trade/:symbol" element={<Trade />} />
          <Route path="/futures" element={<Navigate to="/futures/BTC-PERP" replace />} />
          <Route path="/futures/:symbol" element={<Futures />} />
          <Route path="/wallet" element={<Wallet />} />
          <Route path="/login" element={<Login />} />
          <Route path="/register" element={<Register />} />
          <Route path="*" element={<Navigate to="/markets" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
