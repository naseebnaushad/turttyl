import { useEffect, useRef, type ReactNode } from "react";

/** An inline notice card on glass, shown between the toolbar and the terminal. */
export function Notice({
  tone,
  icon,
  title,
  children,
  actions,
  role = "status",
}: {
  tone: "attention" | "danger" | "neutral" | "accent";
  icon: ReactNode;
  title: ReactNode;
  children?: ReactNode;
  actions?: ReactNode;
  role?: "status" | "alert";
}) {
  return (
    <div className={`notice glass tone-${tone}`} role={role}>
      <span className="notice-icon">{icon}</span>
      <div className="notice-body">
        <div className="notice-title">{title}</div>
        {children && <div className="notice-text">{children}</div>}
      </div>
      {actions && <div className="notice-actions">{actions}</div>}
    </div>
  );
}

export interface MenuItem {
  label: string;
  icon?: ReactNode;
  shortcut?: string;
  destructive?: boolean;
  onSelect: () => void;
}

/** A glass popover menu. Closes on Escape, outside click or selection. */
export function Menu({ items, onClose }: { items: MenuItem[]; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const first = ref.current?.querySelector<HTMLButtonElement>("button");
    first?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        const btns = Array.from(ref.current?.querySelectorAll<HTMLButtonElement>("button") ?? []);
        const i = btns.indexOf(document.activeElement as HTMLButtonElement);
        const next = btns[(i + (e.key === "ArrowDown" ? 1 : btns.length - 1)) % btns.length];
        next?.focus();
        e.preventDefault();
      }
    };
    const click = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    window.addEventListener("keydown", key);
    window.addEventListener("mousedown", click);
    return () => {
      window.removeEventListener("keydown", key);
      window.removeEventListener("mousedown", click);
    };
  }, [onClose]);
  return (
    <div className="menu glass" role="menu" ref={ref}>
      {items.map((it) => (
        <button
          key={it.label}
          role="menuitem"
          className={`menu-item${it.destructive ? " destructive" : ""}`}
          onClick={() => {
            onClose();
            it.onSelect();
          }}
        >
          <span className="menu-icon">{it.icon}</span>
          <span className="menu-label">{it.label}</span>
          {it.shortcut && <span className="menu-shortcut">{it.shortcut}</span>}
        </button>
      ))}
    </div>
  );
}

/** A macOS-style alert sheet: a title, a message, Cancel and a (destructive) action. */
export function AlertSheet({
  title,
  message,
  confirmLabel,
  destructive,
  onConfirm,
  onCancel,
}: {
  title: string;
  message: string;
  confirmLabel: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    cancel.current?.focus(); // destructive actions are never the default
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCancel();
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [onCancel]);
  return (
    <div className="scrim" onMouseDown={onCancel}>
      <div
        className="sheet glass"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="sheet-title"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="sheet-title" id="sheet-title">
          {title}
        </div>
        <div className="sheet-message">{message}</div>
        <div className="sheet-actions">
          <button className="btn" ref={cancel} onClick={onCancel}>
            Cancel
          </button>
          <button className={`btn ${destructive ? "destructive" : "prominent"}`} onClick={onConfirm}>
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
