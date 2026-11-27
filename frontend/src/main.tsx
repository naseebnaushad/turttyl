import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { wailsTransport } from "./transport";
import { isMac } from "./theme";
import "./styles.css";

// On macOS the window sits on native Liquid Glass; elsewhere the sidebar is opaque.
document.documentElement.classList.toggle("glass-window", isMac);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App transport={wailsTransport} />
  </StrictMode>,
);
