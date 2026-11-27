import { useEffect, useState } from "react";
import type { ITheme } from "@xterm/xterm";
import type { DesktopLook } from "./transport";

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

const hex = /^#[0-9a-f]{6}([0-9a-f]{2})?$/i;

/** A colour from the desktop look, re-checked here since it lands in CSS and canvas. */
function pick(look: DesktopLook, ...keys: string[]): string | undefined {
  for (const k of keys) {
    const v = look.colors[k];
    if (v && hex.test(v)) return v;
  }
  return undefined;
}

const ansi = [
  "black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
  "brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightMagenta", "brightCyan", "brightWhite",
] as const;

/** The terminal theme for an appearance; root sessions get a faint red tint. With an
 *  Omarchy look, the theme's own 16 colours, background and cursor are used. */
export function terminalTheme(isDark: boolean, elevated: boolean, look?: DesktopLook | null): ITheme {
  let base = isDark ? dark : light;
  if (look) {
    base = { ...base };
    ansi.forEach((name, i) => {
      const c = pick(look, `color${i}`);
      if (c) base[name] = c;
    });
    const bg = pick(look, "background");
    const fg = pick(look, "foreground");
    const cursor = pick(look, "bright_foreground", "foreground", "accent");
    const sel = pick(look, "selection", "lighter_background");
    if (bg) base.background = base.cursorAccent = bg;
    if (fg) base.foreground = fg;
    if (cursor) base.cursor = cursor;
    if (sel) base.selectionBackground = sel;
    if (!elevated) return base;
    const red = pick(look, "red", "color1") ?? "#ff453a";
    return { ...base, background: mixHex(base.background ?? "#000000", red, 0.06) };
  }
  if (!elevated) return base;
  const bg = isDark ? "#1d1517" : "#fff8f7";
  return { ...base, background: bg, cursorAccent: bg };
}

/** The terminal font: Omarchy's terminal font and text size, else the system mono. */
export function terminalFont(look?: DesktopLook | null): { fontFamily: string; fontSize: number } {
  const fallback = '"SF Mono", ui-monospace, Menlo, "JetBrainsMono Nerd Font", "DejaVu Sans Mono", monospace';
  const fam = look?.fontFamily && /^[A-Za-z0-9 ._+-]{1,64}$/.test(look.fontFamily) ? `"${look.fontFamily}", monospace` : look ? "monospace" : fallback;
  const px = look?.fontPx && look.fontPx >= 9 && look.fontPx <= 20 ? look.fontPx : look ? 13 : 12.5;
  return { fontFamily: fam, fontSize: px };
}

function mixHex(a: string, b: string, t: number): string {
  const p = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
  const [x, y] = [p(a), p(b)];
  return "#" + x.map((v, i) => Math.round(v + (y[i] - v) * t).toString(16).padStart(2, "0")).join("");
}

/** The colour roles the window's CSS reads, from an Omarchy theme. */
const roles: Record<string, string[]> = {
  "--om-bg": ["background"],
  "--om-chrome": ["dark_background", "background"],
  "--om-raised": ["lighter_background", "background"],
  "--om-fg": ["foreground"],
  "--om-fg-dim": ["dark_foreground", "muted", "color8"],
  "--om-muted": ["muted", "color8"],
  "--om-accent": ["accent", "color4"],
  "--om-red": ["red", "color1"],
  "--om-green": ["green", "color2"],
  "--om-orange": ["orange", "yellow", "color3"],
  "--om-selection": ["selection", "lighter_background"],
};

/** Puts the Omarchy look on the page (or takes it off): colour roles, font and size. */
export function applyLook(look: DesktopLook | null) {
  const root = document.documentElement;
  root.classList.toggle("omarchy", !!look);
  for (const [prop, keys] of Object.entries(roles)) {
    const v = look ? pick(look, ...keys) : undefined;
    if (v) root.style.setProperty(prop, v);
    else root.style.removeProperty(prop);
  }
  const font = terminalFont(look);
  if (look) {
    root.style.setProperty("--om-font", font.fontFamily);
    root.style.setProperty("--om-size", `${font.fontSize}px`);
  } else {
    root.style.removeProperty("--om-font");
    root.style.removeProperty("--om-size");
  }
}

/** Follows the desktop look: loads it once and updates on change. */
export function useDesktopLook(t: {
  appearance(): Promise<DesktopLook | null>;
  onAppearance(cb: (l: DesktopLook | null) => void): () => void;
}): DesktopLook | null {
  const [look, setLook] = useState<DesktopLook | null>(null);
  useEffect(() => {
    let live = true;
    t.appearance()
      .then((l) => live && setLook(l))
      .catch(() => {});
    const off = t.onAppearance(setLook);
    return () => {
      live = false;
      off();
    };
  }, [t]);
  useEffect(() => applyLook(look), [look]);
  return look;
}
