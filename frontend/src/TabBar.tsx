import { useState, type ReactNode } from "react";
import type { AppInfo, SessionInfo } from "./transport";
import { CloseIcon, HandIcon, PlusIcon, ShieldIcon, SparkIcon, TerminalIcon } from "./Icons";
import { Menu, type MenuItem } from "./Overlays";
import { isMac } from "./theme";
import { keyLabel } from "./shortcuts";

interface Props {
  info: AppInfo | null;
  sessions: SessionInfo[];
  activeId: string | null;
  onSelect: (id: string) => void;
  onCreate: (elevated: boolean) => void;
  onClose: (s: SessionInfo) => void;
  /** Status of the selected session, shown at the right end of the bar. */
  children?: ReactNode;
}

/** Animated "…" shown while an agent types. */
export function TypingDots() {
  return (
    <span className="typing-dots" aria-hidden="true">
      <i />
      <i />
      <i />
    </span>
  );
}

function TabIcon({ s }: { s: SessionInfo }) {
  if (s.attention) return <HandIcon size={14} className="tab-icon asking" />;
  if (s.elevated) return <ShieldIcon size={14} className="tab-icon root" />;
  if (s.createdBy.kind === "agent") return <SparkIcon size={14} className="tab-icon agent" />;
  return <TerminalIcon size={14} className="tab-icon" />;
}

/** Browser-style session tabs in the window's title bar, with the new-session menu. */
export function TabBar({ info, sessions, activeId, onSelect, onCreate, onClose, children }: Props) {
  const [menu, setMenu] = useState(false);
  const items: MenuItem[] = [
    { label: "New Session", icon: <TerminalIcon />, shortcut: keyLabel.newTab, onSelect: () => onCreate(false) },
  ];
  if (info?.elevationEnabled) {
    items.push({ label: "New Elevated Session…", icon: <ShieldIcon />, destructive: true, onSelect: () => onCreate(true) });
  }

  return (
    <header className={`tabbar drag${isMac ? " inset" : ""}`}>
      <div className="tabs" role="tablist" aria-label="Sessions">
        {sessions.map((s, i) => {
          const selected = s.id === activeId;
          const who = s.typing ? `${s.typing} is typing` : s.createdBy.kind === "agent" ? `opened by ${s.createdBy.name}` : "";
          return (
            <div
              key={s.id}
              role="tab"
              tabIndex={0}
              aria-selected={selected}
              className={`tab no-drag${selected ? " selected" : ""}${s.state === "exited" ? " ended" : ""}${s.attention ? " asking" : ""}${s.elevated ? " elevated" : ""}`}
              onClick={() => onSelect(s.id)}
              onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && onSelect(s.id)}
              onAuxClick={(e) => e.button === 1 && onClose(s)}
              title={[s.name, s.cwd, who, i < 9 ? keyLabel.tab(i + 1) : ""].filter(Boolean).join(" — ")}
            >
              <TabIcon s={s} />
              <span className="tab-name">{s.name}</span>
              {s.typing && <TypingDots />}
              <button
                className="tab-close"
                tabIndex={-1}
                aria-label={`Close ${s.name}`}
                onClick={(e) => {
                  e.stopPropagation();
                  onClose(s);
                }}
              >
                <CloseIcon size={12} />
              </button>
            </div>
          );
        })}
        <div className="menu-anchor no-drag">
          <button
            className="icon-btn new-tab"
            onClick={() => setMenu((m) => !m)}
            aria-haspopup="menu"
            aria-expanded={menu}
            title="New session"
          >
            <PlusIcon />
          </button>
          {menu && <Menu items={items} onClose={() => setMenu(false)} />}
        </div>
      </div>

      <div className="tabbar-status no-drag">{children}</div>

      {info?.elevationEnabled && (
        <span className="tabbar-warn" title={`Elevated sessions are enabled in config.json · v${info.version}`}>
          <ShieldIcon size={12} /> Elevation on
        </span>
      )}
    </header>
  );
}

/** "~/projects/app" style path for display. */
export function shortPath(p: string): string {
  const m = p.match(/^\/(Users|home)\/[^/]+(.*)$/);
  return m ? "~" + m[2] : p;
}
