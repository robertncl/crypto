import React from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { AuthProvider } from "./state/auth";
import { wsClient } from "./api/ws";
import "./index.css";

// Open the shared market-data socket as soon as the app boots; the auth layer
// upgrades it with a token once a session is restored or created.
wsClient.start();

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <AuthProvider>
      <App />
    </AuthProvider>
  </React.StrictMode>,
);
