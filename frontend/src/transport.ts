// The one place the UI talks to Go. Today it uses the Wails bridge; a WebSocket
// transport for browser mode can implement the same interface later (CR D9).
import { Call, Events } from "@wailsio/runtime";

export type State = "starting" | "authenticating" | "ready" | "exited";

export interface Actor {
  kind: "human" | "agent";
  name: string;
}

export interface SessionInfo {
  id: string;
  name: string;
  cwd: string;
  elevated: boolean;
  state: State;
  createdBy: Actor;
  exitCode?: number;
  typing?: string;
  running: boolean;
  passwordPrompt?: boolean;
  attention?: Attention;
}

/** An agent waiting for the person to act in a session. */
export interface Attention {
  kind: "password" | "input";
  agent: string;
  reason: string;
  since: string;
}

export interface AppInfo {
  version: string;
  elevationEnabled: boolean;
}

/** The Omarchy desktop's look (Linux): theme colours, mode, terminal font and text size. */
export interface DesktopLook {
  name: string;
  mode: "dark" | "light";
  colors: Record<string, string>;
  fontFamily?: string;
  fontPx?: number;
}

export interface OutputEvent {
  id: string;
  offset: number;
  data: Uint8Array;
}

export interface Snapshot {
  data: Uint8Array;
  offset: number;
}

export interface Transport {
  info(): Promise<AppInfo>;
  list(): Promise<SessionInfo[]>;
  create(name: string, cols: number, rows: number): Promise<SessionInfo>;
  createElevated(name: string, cols: number, rows: number): Promise<SessionInfo>;
  snapshot(id: string): Promise<Snapshot>;
  write(id: string, data: string): Promise<void>;
  resize(id: string, cols: number, rows: number): Promise<void>;
  close(id: string): Promise<void>;
  handBack(id: string): Promise<void>;
  onOutput(cb: (ev: OutputEvent) => void): () => void;
  onSessions(cb: (s: SessionInfo[]) => void): () => void;
  /** The desktop look to follow, or null for the app's own look. */
  appearance(): Promise<DesktopLook | null>;
  onAppearance(cb: (look: DesktopLook | null) => void): () => void;
}

const SVC = "github.com/naseebnaushad/turttyl/internal/adapters/desktop.TerminalService.";

function b64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function call<T>(method: string, ...args: unknown[]): Promise<T> {
  return Call.ByName(SVC + method, ...args) as Promise<T>;
}

export const wailsTransport: Transport = {
  info: () => call<AppInfo>("Info"),
  list: async () => (await call<SessionInfo[] | null>("List")) ?? [],
  create: (name, cols, rows) => call<SessionInfo>("Create", name, cols, rows),
  createElevated: (name, cols, rows) => call<SessionInfo>("CreateElevated", name, cols, rows),
  snapshot: async (id) => {
    const s = await call<{ data: string; offset: number }>("Snapshot", id);
    return { data: b64(s.data), offset: s.offset };
  },
  write: (id, data) => call<void>("Write", id, data),
  resize: (id, cols, rows) => call<void>("Resize", id, cols, rows),
  close: (id) => call<void>("Close", id),
  handBack: (id) => call<void>("HandBack", id),
  onOutput: (cb) =>
    Events.On("terminal:output", (ev: { data: unknown }) => {
      const d = ev.data as { id: string; offset: number; data: string };
      cb({ id: d.id, offset: d.offset, data: b64(d.data) });
    }),
  onSessions: (cb) =>
    Events.On("terminal:sessions", (ev: { data: unknown }) => cb((ev.data as SessionInfo[] | null) ?? [])),
  appearance: async () => asLook(await call<unknown>("Appearance")),
  onAppearance: (cb) => Events.On("app:appearance", (ev: { data: unknown }) => cb(asLook(ev.data))),
};

/** Accepts only a well-formed desktop look; anything else (null, {}, junk) means none. */
export function asLook(v: unknown): DesktopLook | null {
  if (!v || typeof v !== "object") return null;
  const o = v as Partial<DesktopLook>;
  if (!o.colors || typeof o.colors !== "object" || Array.isArray(o.colors)) return null;
  const colors: Record<string, string> = {};
  for (const [k, c] of Object.entries(o.colors)) if (typeof c === "string") colors[k] = c;
  return {
    name: typeof o.name === "string" ? o.name : "",
    mode: o.mode === "light" ? "light" : "dark",
    colors,
    fontFamily: typeof o.fontFamily === "string" ? o.fontFamily : undefined,
    fontPx: typeof o.fontPx === "number" ? o.fontPx : undefined,
  };
}

/** Turns an unknown error from a Go call into a short message for the person. */
export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "string") return e;
  if (e && typeof e === "object" && "message" in e) return String((e as { message: unknown }).message);
  return "Something went wrong.";
}
