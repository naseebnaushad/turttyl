import { isMac } from "./theme";

// Window shortcuts per platform. On Linux the Super key belongs to the window manager
// (Hyprland on Omarchy), so turttyl uses the usual terminal shortcuts there instead.

export type AppShortcut = { kind: "new" } | { kind: "switch"; index: number } | null;

/** The window shortcut a key press means, if any. xterm leaves these alone. */
export function appShortcut(e: KeyboardEvent): AppShortcut {
  const digit = /^Digit([1-9])$/.exec(e.code);
  if (isMac) {
    if (!e.metaKey || e.altKey || e.ctrlKey) return null;
    if (e.key.toLowerCase() === "t" && !e.shiftKey) return { kind: "new" };
    if (digit && !e.shiftKey) return { kind: "switch", index: Number(digit[1]) - 1 };
    return null;
  }
  if (e.ctrlKey && e.shiftKey && !e.altKey && !e.metaKey && e.code === "KeyT") return { kind: "new" };
  if (e.altKey && !e.ctrlKey && !e.shiftKey && !e.metaKey && digit) return { kind: "switch", index: Number(digit[1]) - 1 };
  return null;
}

/** Labels shown in menus, tooltips and buttons. */
export const keyLabel = {
  newTab: isMac ? "⌘T" : "Ctrl+Shift+T",
  tab: (n: number) => (isMac ? `⌘${n}` : `Alt+${n}`),
  handBack: isMac ? "⌘↵" : "Ctrl+Shift+↵",
};
