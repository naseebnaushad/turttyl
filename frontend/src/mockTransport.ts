// Development only: a fake backend so the UI can be laid out and reviewed in a normal
// browser (`npm run dev`, then open /?mock, /?mock=omarchy or /?mock=omarchy-light).
// main.tsx loads it only when import.meta.env.DEV is true, so it never ships.
import type { DesktopLook, OutputEvent, SessionInfo, Transport } from "./transport";

const enc = new TextEncoder();

const prompt = "\x1b[32muser@host\x1b[0m ~/projects/app % ";
const sample = [
  prompt + "ls",
  "README.md    cmd    documents    frontend    go.mod    internal",
  prompt + "git status --short",
  " M frontend/src/App.tsx",
  " M frontend/src/styles.css",
  prompt + "make test",
  "ok   internal/app        5.4s",
  "ok   internal/adapters   1.6s",
  prompt + "read -r \"a?Delete 3 old releases? [y/N] \"",
  "Delete 3 old releases? [y/N] ",
].join("\r\n");

const darkLook: DesktopLook = {
  name: "synthetic-dark",
  mode: "dark",
  fontPx: 14,
  colors: {
    accent: "#7aa2f7", selection: "#283457", muted: "#414868",
    background: "#1a1b26", dark_background: "#16161e", lighter_background: "#24283b",
    foreground: "#c0caf5", dark_foreground: "#565f89", bright_foreground: "#c0caf5",
    red: "#f7768e", green: "#9ece6a", orange: "#ff9e64",
    color0: "#15161e", color1: "#f7768e", color2: "#9ece6a", color3: "#e0af68",
    color4: "#7aa2f7", color5: "#bb9af7", color6: "#7dcfff", color7: "#a9b1d6", color8: "#414868",
  },
};

const lightLook: DesktopLook = {
  name: "synthetic-light",
  mode: "light",
  fontPx: 14,
  colors: {
    accent: "#3d7a5c", selection: "#dfe6d8", muted: "#b9bfb3",
    background: "#f6f4ee", dark_background: "#ebe8df", lighter_background: "#ffffff",
    foreground: "#2f3430", dark_foreground: "#7b8379", bright_foreground: "#1c1f1d",
    red: "#b8443a", green: "#3d7a5c", orange: "#c4721f",
    color0: "#2f3430", color1: "#b8443a", color2: "#3d7a5c", color3: "#9a6b12",
    color4: "#2f6690", color5: "#8a4f7d", color6: "#2a7f86", color7: "#7b8379",
  },
};

/** A fake transport with three sessions: a person's, an agent's that needs the person,
 *  and an unlocked root session. */
export function mockTransport(variant: string): Transport {
  const now = new Date().toISOString();
  let sessions: SessionInfo[] = [
    { id: "s1", name: "session 2e39", cwd: "/home/user/projects/app", elevated: false, state: "ready", createdBy: { kind: "human", name: "you" }, running: false },
    {
      id: "s2", name: "deploy", cwd: "/home/user/projects/app", elevated: false, state: "ready",
      createdBy: { kind: "agent", name: "demo-agent" }, running: true,
      attention: { kind: "input", agent: "demo-agent", reason: "Decide whether to delete the 3 old releases.", since: now },
    },
    { id: "s3", name: "prod maintenance", cwd: "/root", elevated: true, state: "ready", createdBy: { kind: "human", name: "you" }, running: false, typing: "demo-agent" },
  ];
  const listeners = new Set<(s: SessionInfo[]) => void>();
  const emit = () => listeners.forEach((cb) => cb(sessions));
  const look = variant === "omarchy" ? darkLook : variant === "omarchy-light" ? lightLook : null;
  let n = 4;

  return {
    info: async () => ({ version: "0.1.0-alpha.mock", elevationEnabled: true }),
    list: async () => sessions,
    create: async (name) => {
      const s: SessionInfo = { id: `s${n}`, name: name || `session ${n++}a1`, cwd: "/home/user", elevated: false, state: "ready", createdBy: { kind: "human", name: "you" }, running: false };
      sessions = [...sessions, s];
      emit();
      return s;
    },
    createElevated: async () => {
      throw new Error("Elevated sessions need the real app.");
    },
    snapshot: async (id) => ({ data: enc.encode(id === "s2" ? sample : sample.split("\r\n").slice(0, 8).join("\r\n") + "\r\n" + prompt), offset: 0 }),
    write: async () => {},
    resize: async () => {},
    close: async (id) => {
      sessions = sessions.filter((s) => s.id !== id);
      emit();
    },
    handBack: async (id) => {
      sessions = sessions.map((s) => (s.id === id ? { ...s, attention: undefined } : s));
      emit();
    },
    onOutput: (_cb: (ev: OutputEvent) => void) => () => {},
    onSessions: (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    appearance: async () => look,
    onAppearance: () => () => {},
  };
}
