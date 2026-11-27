import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { TerminalView, type OutputRouter } from "./TerminalView";
import { errorText, type AppInfo, type SessionInfo, type Transport } from "./transport";
import { TabBar, TypingDots, shortPath } from "./TabBar";
import { AlertSheet, Notice } from "./Overlays";
import { ArrowUturnIcon, HandIcon, InfoIcon, KeyIcon, PlusIcon, ShieldIcon, TerminalIcon } from "./Icons";
import { useDarkMode, useDesktopLook } from "./theme";
import { appShortcut, keyLabel } from "./shortcuts";

interface Props {
  transport: Transport;
}

/** How long after the person's last keystroke the app may switch tabs by itself. */
const TYPING_GRACE_MS = 5000;

/** The window: session tabs in the glass title bar and the selected terminal on a content card. */
export function App({ transport }: Props) {
  const [info, setInfo] = useState<AppInfo | null>(null);
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirmClose, setConfirmClose] = useState<SessionInfo | null>(null);
  const look = useDesktopLook(transport);
  const systemDark = useDarkMode();
  const dark = look ? look.mode === "dark" : systemDark;
  const router = useMemo<OutputRouter>(() => new Map(), []);
  const known = useRef<Set<string>>(new Set());
  const asking = useRef<Set<string>>(new Set()); // sessions whose request we've seen
  const lastKey = useRef(0);

  const showError = useCallback((msg: string) => {
    setError(msg);
    window.setTimeout(() => setError((cur) => (cur === msg ? null : cur)), 6000);
  }, []);

  // Switch tabs for the person only when they are not in the middle of typing, so
  // their keystrokes never land in a different terminal than they meant.
  const mayJump = () => Date.now() - lastKey.current > TYPING_GRACE_MS;

  const applySessions = useCallback((list: SessionInfo[]) => {
    setSessions(list);
    const fresh = list.filter((s) => !known.current.has(s.id));
    known.current = new Set(list.map((s) => s.id));
    const newAsks = list.filter((s) => s.attention && !asking.current.has(s.id));
    asking.current = new Set(list.filter((s) => s.attention).map((s) => s.id));
    setActiveId((cur) => {
      const jump = newAsks[0] ?? (fresh.length ? fresh[fresh.length - 1] : undefined);
      if (jump && (cur === null || mayJump())) return jump.id;
      if (cur && list.some((s) => s.id === cur)) return cur;
      return list.length ? list[list.length - 1].id : null;
    });
  }, []);

  useEffect(() => {
    const offOut = transport.onOutput((ev) => router.get(ev.id)?.(ev));
    const offList = transport.onSessions(applySessions);
    transport.info().then(setInfo).catch((e) => showError(errorText(e)));
    transport
      .list()
      .then(async (list) => {
        if (list.length === 0) {
          await transport.create("", 120, 32); // the list event selects it
        } else {
          applySessions(list);
        }
      })
      .catch((e) => showError(errorText(e)));
    return () => {
      offOut();
      offList();
    };
  }, [transport, router, applySessions, showError]);

  const create = useCallback(
    async (elevated: boolean) => {
      try {
        const s = elevated ? await transport.createElevated("", 120, 32) : await transport.create("", 120, 32);
        setActiveId(s.id);
      } catch (e) {
        showError(errorText(e));
      }
    },
    [transport, showError],
  );

  const doClose = async (id: string) => {
    setConfirmClose(null);
    try {
      await transport.close(id);
    } catch (e) {
      showError(errorText(e));
    }
  };

  const askClose = (s: SessionInfo) => (s.state === "exited" ? doClose(s.id) : setConfirmClose(s));

  const handBack = useCallback(
    (id: string) => {
      transport.handBack(id).catch((e) => showError(errorText(e)));
    },
    [transport, showError],
  );

  // Window shortcuts: new session and switch session (see shortcuts.ts per platform).
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      const sc = appShortcut(e);
      if (sc?.kind === "new") {
        e.preventDefault();
        create(false);
      } else if (sc?.kind === "switch") {
        const s = sessions[sc.index];
        if (s) {
          e.preventDefault();
          setActiveId(s.id);
        }
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [sessions, create]);

  const active = sessions.find((s) => s.id === activeId) ?? null;
  const elsewhere = sessions.filter((s) => s.attention && s.id !== activeId);

  const status = active && (
    <>
      <span className="status-text">
        {active.typing ? (
          <span className="typing">
            {active.typing} is typing <TypingDots />
          </span>
        ) : active.state === "exited" ? (
          `Ended${active.exitCode !== undefined ? ` · exit ${active.exitCode}` : ""}`
        ) : (
          <>
            {active.createdBy.kind === "agent" && <span className="owner path">{active.createdBy.name}</span>}
            <span className="path">{shortPath(active.cwd)}</span>
            {active.running && <span className="dot-sep">Running</span>}
          </>
        )}
      </span>
      {active.elevated && (
        <span className="pill root">
          <ShieldIcon size={11} /> Root
        </span>
      )}
    </>
  );

  return (
    <div className={`window${dark ? " dark" : ""}`}>
      <TabBar
        info={info}
        sessions={sessions}
        activeId={activeId}
        onSelect={setActiveId}
        onCreate={create}
        onClose={askClose}
      >
        {status}
      </TabBar>

      <main className={`content${active?.elevated ? " elevated" : ""}${active?.attention ? " asking" : ""}`}>
        <div className="notices">
          {active?.attention?.kind === "input" && (
            <Notice
              tone="attention"
              role="alert"
              icon={<HandIcon size={18} />}
              title={`${active.attention.agent} needs you`}
              actions={
                <button className="btn attention" onClick={() => handBack(active.id)} title={`Hand back (${keyLabel.handBack})`}>
                  <ArrowUturnIcon size={13} /> Hand Back <kbd>{keyLabel.handBack}</kbd>
                </button>
              }
            >
              {active.attention.reason} Do it in the terminal below, then hand back so {active.attention.agent} can carry on.
            </Notice>
          )}
          {active?.attention?.kind === "password" && (
            <Notice
              tone="attention"
              role="alert"
              icon={<KeyIcon size={18} />}
              title="Password needed"
              actions={
                <button className="btn" onClick={() => handBack(active.id)} title="Stop waiting for the password">
                  Skip
                </button>
              }
            >
              Type it in the terminal below. {active.attention.agent} can't see it and carries on once it's accepted.
            </Notice>
          )}
          {active && !active.attention && active.passwordPrompt && active.state === "ready" && (
            <Notice tone="neutral" icon={<KeyIcon size={18} />} title="Waiting for a password">
              Agents can't type passwords. You can.
            </Notice>
          )}
          {active?.elevated && active.state === "authenticating" && (
            <Notice tone="attention" icon={<ShieldIcon size={18} />} title="Unlock this root session">
              Type your sudo password in the terminal below. Agents can't see or use this session until then.
            </Notice>
          )}
          {active?.elevated && active.state === "ready" && (
            <Notice tone="danger" icon={<ShieldIcon size={18} />} title="Root session">
              Agents can run commands here as the superuser without a password. It closes when you close it, when the app
              quits, or after it has been idle for a while.
            </Notice>
          )}
          {active?.state === "exited" && (
            <Notice tone="neutral" icon={<InfoIcon size={18} />} title="Session ended">
              The shell exited{active.exitCode !== undefined ? ` with code ${active.exitCode}` : ""}. Its output stays here
              until you close it.
            </Notice>
          )}
        </div>

        <div className="term-area">
          {sessions.map((s) => (
            <TerminalView
              key={s.id}
              id={s.id}
              active={s.id === activeId}
              elevated={s.elevated}
              transport={transport}
              router={router}
              onError={showError}
              onUserInput={() => (lastKey.current = Date.now())}
              onHandBack={() => s.attention && handBack(s.id)}
              attentionKey={s.attention?.since}
              dark={dark}
              look={look}
            />
          ))}
          {sessions.length === 0 && (
            <div className="empty">
              <TerminalIcon size={40} className="empty-icon" />
              <div className="empty-title">No Sessions</div>
              <div className="empty-text">Start a session, or let an agent open one.</div>
              <button className="btn prominent" onClick={() => create(false)}>
                <PlusIcon size={14} /> New Session
              </button>
            </div>
          )}
        </div>

        {elsewhere.length > 0 && (
          <div className="toast glass tone-attention" role="status">
            <HandIcon size={16} />
            <span>
              <strong>{elsewhere[0].attention!.agent}</strong> needs you in <strong>{elsewhere[0].name}</strong>
              {elsewhere.length > 1 ? ` and ${elsewhere.length - 1} more` : ""}
            </span>
            <button className="btn small" onClick={() => setActiveId(elsewhere[0].id)}>
              Show
            </button>
          </div>
        )}
        {error && !elsewhere.length && (
          <div className="toast glass tone-danger" role="alert">
            <InfoIcon size={16} />
            <span>{error}</span>
          </div>
        )}
      </main>

      {confirmClose && (
        <AlertSheet
          title={`Close “${confirmClose.name}”?`}
          message={
            confirmClose.elevated
              ? "The root shell and anything running in it will be stopped."
              : "Anything running in this session will be stopped."
          }
          confirmLabel="Close Session"
          destructive
          onConfirm={() => doClose(confirmClose.id)}
          onCancel={() => setConfirmClose(null)}
        />
      )}
    </div>
  );
}
