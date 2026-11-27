import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { wailsTransport, type Transport } from "./transport";
import { isMac } from "./theme";
import "./styles.css";

// On macOS the window sits on native Liquid Glass; elsewhere the page paints its own background.
document.documentElement.classList.toggle("glass-window", isMac);

async function pickTransport(): Promise<Transport> {
  // Development only: /?mock runs the UI on fake data in a normal browser.
  const mock = import.meta.env.DEV ? new URLSearchParams(location.search).get("mock") : null;
  if (mock === null) return wailsTransport;
  document.documentElement.classList.add("mock");
  return (await import("./mockTransport")).mockTransport(mock);
}

pickTransport().then((transport) =>
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <App transport={transport} />
    </StrictMode>,
  ),
);
