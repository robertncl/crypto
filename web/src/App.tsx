import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { Layout } from "./components/Layout";
import { Markets } from "./pages/Markets";
import { Trade } from "./pages/Trade";
import { Futures } from "./pages/Futures";
import { Wallet } from "./pages/Wallet";
import { Login } from "./pages/Login";
import { Register } from "./pages/Register";

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
