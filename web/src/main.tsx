import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "@telegram-apps/telegram-ui/dist/styles.css";

import App from "./App";
import { host } from "./host";

import "./styles.css";

// Telegram theme variables must be on <html> before the first paint; the auth
// layer repeats the call after React mounts (Telegram's ready/expand).
host.init();

const container = document.getElementById("root");
if (!container) {
  throw new Error("root container is missing in index.html");
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
