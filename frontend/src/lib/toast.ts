export interface ToastOptions {
  /** How long it stays, in milliseconds; errors default to longer. */
  ms?: number;
  /** A button on the toast. A toast with one is not dismissed by clicking it. */
  action?: { label: string; run: () => void };
}

/** Fire a toast. Toasts.tsx listens for this event, so any component can
    raise one without threading a callback down the tree. */
export function toast(kind: "info" | "error" | "success", text: string, opts: ToastOptions = {}) {
  window.dispatchEvent(new CustomEvent("ws:toast", { detail: { kind, text, ...opts } }));
}
