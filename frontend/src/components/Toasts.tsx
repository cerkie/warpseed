import { useEffect, useState } from "react";
import { on } from "../ipc";
import { Check, Disc, Warning } from "./Icon";

interface Toast {
  id: number;
  kind: "info" | "error" | "success";
  text: string;
  action?: { label: string; run: () => void };
}

let nextId = 1;

/** Bottom-right toasts (ux-spec §7.8): app errors + local notifications via
    the ws:toast CustomEvent. Max 3 shown; click one to dismiss it. Auto-dismiss
    after 5s, errors after 12s. */
export default function Toasts() {
  const [toasts, setToasts] = useState<Toast[]>([]);

  useEffect(() => {
    const push = (kind: Toast["kind"], text: string, ms?: number, action?: Toast["action"]) => {
      const t = { id: nextId++, kind, text, action };
      setToasts((ts) => [...ts.slice(-2), t]);
      // Errors carry what the user must act on, so they linger.
      setTimeout(() => setToasts((ts) => ts.filter((x) => x.id !== t.id)), ms ?? (kind === "error" ? 12000 : 5000));
    };
    const offErr = on<string>("app:error", (msg) => push("error", msg));
    const offInfo = on<string>("app:info", (msg) => push("success", msg));
    const local = (ev: Event) => {
      const { kind, text, ms, action } = (
        ev as CustomEvent<{ kind: Toast["kind"]; text: string; ms?: number; action?: Toast["action"] }>
      ).detail;
      push(kind, text, ms, action);
    };
    window.addEventListener("ws:toast", local);
    return () => {
      offErr();
      offInfo();
      window.removeEventListener("ws:toast", local);
    };
  }, []);

  if (toasts.length === 0) return null;
  return (
    <div className="toasts">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`toast toast--${t.kind}`}
          role={t.kind === "error" ? "alert" : "status"}
          title={t.action ? undefined : "Click to dismiss"}
          onClick={() => !t.action && setToasts((ts) => ts.filter((x) => x.id !== t.id))}
        >
          <span className="toast__icon">
            {t.kind === "error" ? (
              <Warning size={15} />
            ) : t.kind === "success" ? (
              <Check size={15} />
            ) : (
              <Disc size={15} />
            )}
          </span>
          <span className="toast__text">{t.text}</span>
          {t.action && (
            <button
              className="toast__action"
              onClick={() => {
                t.action?.run();
                setToasts((ts) => ts.filter((x) => x.id !== t.id));
              }}
            >
              {t.action.label}
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
