import { useEffect, useState } from "react";
import type { ITheme } from "@xterm/xterm";

/** Follows the system light/dark appearance. */
export function useDarkMode(): boolean {
  const q = "(prefers-color-scheme: dark)";
  const [dark, setDark] = useState(() => window.matchMedia(q).matches);
  useEffect(() => {
    const m = window.matchMedia(q);
    const on = (e: MediaQueryListEvent) => setDark(e.matches);
    m.addEventListener("change", on);
    return () => m.removeEventListener("change", on);
  }, []);
  return dark;
}

/** True on macOS, where the window sits on native Liquid Glass. */
export const isMac = /Mac/.test(navigator.userAgent);

// Terminal palettes tuned for legibility on the content surface in each appearance.
const dark: ITheme = {
  background: "#17171a",
  foreground: "#e5e5ea",
  cursor: "#0a84ff",
  cursorAccent: "#17171a",
  selectionBackground: "rgba(10, 132, 255, 0.35)",
  black: "#3a3a3c",
  red: "#ff6961",
  green: "#30d158",
  yellow: "#ffd60a",
  blue: "#409cff",
  magenta: "#da8fff",
  cyan: "#70d7ff",
  white: "#d1d1d6",
  brightBlack: "#636366",
  brightRed: "#ff8a84",
  brightGreen: "#6be38a",
  brightYellow: "#ffe45c",
  brightBlue: "#70b5ff",
  brightMagenta: "#e5adff",
  brightCyan: "#9be4ff",
  brightWhite: "#f2f2f7",
};

const light: ITheme = {
  background: "#ffffff",
  foreground: "#1d1d1f",
  cursor: "#007aff",
  cursorAccent: "#ffffff",
  selectionBackground: "rgba(0, 122, 255, 0.22)",
  black: "#1d1d1f",
  red: "#c41a16",
  green: "#1a7f37",
  yellow: "#8a6100",
  blue: "#0b5cd5",
  magenta: "#a2369c",
  cyan: "#0e7490",
  white: "#8e8e93",
  brightBlack: "#636366",
  brightRed: "#e0352b",
  brightGreen: "#248a3d",
  brightYellow: "#a87800",
  brightBlue: "#007aff",
  brightMagenta: "#bf40bf",
  brightCyan: "#0891b2",
  brightWhite: "#3a3a3c",
};

/** The terminal theme for an appearance; root sessions get a faint red tint. */
export function terminalTheme(isDark: boolean, elevated: boolean): ITheme {
  const base = isDark ? dark : light;
  if (!elevated) return base;
  const bg = isDark ? "#1d1517" : "#fff8f7";
  return { ...base, background: bg, cursorAccent: bg };
}
