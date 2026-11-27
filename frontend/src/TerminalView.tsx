import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import type { OutputEvent, Transport } from "./transport";
import { terminalTheme } from "./theme";

/** Routes live output to the terminal view that owns each session. */
export type OutputRouter = Map<string, (ev: OutputEvent) => void>;

interface Props {
  id: string;
  active: boolean;
  elevated: boolean;
  transport: Transport;
  router: OutputRouter;
  onError: (msg: string) => void;
  /** Called on every keystroke the person sends (to know when they are busy typing). */
  onUserInput: () => void;
  /** ⌘↵ / Ctrl+Shift+↵: hand the session back to the waiting agent. */
  onHandBack: () => void;
  /** Changes when a new request for the person arrives, to focus the terminal. */
  attentionKey?: string;
  /** System dark appearance. */
  dark: boolean;
}

/** App shortcuts that xterm must leave alone so they reach the window. */
function isAppShortcut(e: KeyboardEvent): boolean {
  return e.metaKey && !e.altKey && (e.key === "t" || /^[1-9]$/.test(e.key));
}

/**
 * One xterm.js terminal bound to a session. It stays mounted while hidden so its
 * screen state survives switching tabs. It draws the kept history first, then applies
 * live output after that offset, so nothing is shown twice or lost.
 */
export function TerminalView({ id, active, elevated, transport, router, onError, onUserInput, onHandBack, attentionKey, dark }: Props) {
  const host = useRef<HTMLDivElement>(null);
  // Latest callbacks, read from xterm handlers that are set up once.
  const cb = useRef({ onUserInput, onHandBack });
  cb.current = { onUserInput, onHandBack };
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);

  useEffect(() => {
    const t = new Terminal({
      // Same size the shell starts with, so history drawn before the first fit lines up.
      cols: 120,
      rows: 32,
      fontFamily: '"SF Mono", ui-monospace, Menlo, "DejaVu Sans Mono", monospace',
      fontSize: 12.5,
      lineHeight: 1.18,
      cursorBlink: true,
      cursorStyle: "bar",
      cursorWidth: 2,
      scrollback: 10000,
      allowProposedApi: false,
      macOptionIsMeta: true,
      theme: terminalTheme(dark, elevated),
    });
    const f = new FitAddon();
    t.loadAddon(f);
    t.open(host.current!);
    term.current = t;
    fit.current = f;

    let drawnTo = -1; // offset up to which output has been drawn; -1 until the snapshot
    const queued: OutputEvent[] = [];
    const apply = (ev: OutputEvent) => {
      const end = ev.offset + ev.data.length;
      if (end <= drawnTo) return;
      const skip = Math.max(0, drawnTo - ev.offset);
      t.write(skip ? ev.data.subarray(skip) : ev.data);
      drawnTo = end;
    };
    router.set(id, (ev) => (drawnTo < 0 ? queued.push(ev) : apply(ev)));

    transport
      .snapshot(id)
      .then((snap) => {
        if (host.current && host.current.offsetParent !== null) f.fit();
        t.write(snap.data);
        drawnTo = snap.offset + snap.data.length;
        queued.splice(0).forEach(apply);
      })
      .catch((e) => onError(String(e)));

    t.attachCustomKeyEventHandler((e) => {
      const handBack = e.key === "Enter" && (e.metaKey || (e.ctrlKey && e.shiftKey));
      if (handBack) {
        if (e.type === "keydown") cb.current.onHandBack();
        return false;
      }
      return !isAppShortcut(e);
    });
    const input = t.onData((d) => {
      cb.current.onUserInput();
      transport.write(id, d).catch(() => {});
    });
    const resize = t.onResize(({ cols, rows }) => transport.resize(id, cols, rows).catch(() => {}));
    const ro = new ResizeObserver(() => {
      if (host.current && host.current.offsetParent !== null) f.fit();
    });
    ro.observe(host.current!);

    return () => {
      ro.disconnect();
      input.dispose();
      resize.dispose();
      router.delete(id);
      t.dispose();
    };
    // The session id never changes for a mounted view.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  useEffect(() => {
    if (active && term.current) {
      requestAnimationFrame(() => {
        fit.current?.fit();
        term.current?.focus();
      });
    }
  }, [active]);

  useEffect(() => {
    if (active && attentionKey) term.current?.focus();
  }, [active, attentionKey]);

  useEffect(() => {
    if (term.current) term.current.options.theme = terminalTheme(dark, elevated);
  }, [dark, elevated]);

  return <div className="term-host" ref={host} style={{ display: active ? "block" : "none" }} />;
}
