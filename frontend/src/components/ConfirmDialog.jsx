import { useEffect, useId, useRef } from "react";
import { Button } from "@/components/kx";

/**
 * Minimal token-styled confirm modal built on <dialog>: focus trap, Esc to cancel,
 * backdrop, no extra UI library.
 */
export default function ConfirmDialog({
  open,
  title,
  children,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  tone = "danger",
  busy = false,
  onConfirm,
  onCancel,
}) {
  const ref = useRef(null);
  const titleId = useId();

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      if (typeof d.showModal === "function") d.showModal();
      else d.setAttribute("open", "");
    } else if (!open && d.open) {
      if (typeof d.close === "function") d.close();
      else d.removeAttribute("open");
    }
  }, [open]);

  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      data-testid="confirm-dialog"
      onCancel={(e) => {
        e.preventDefault();
        onCancel?.();
      }}
      className="m-auto w-[min(92vw,420px)] rounded-card border border-kx-line bg-kx-surface p-0 text-kx-ink shadow-2xl backdrop:bg-kx-bg/75"
    >
      {open && (
        <div className="p-5">
          <h2 id={titleId} className="text-base font-semibold text-kx-ink">
            {title}
          </h2>
          <div className="mt-2 text-sm leading-relaxed text-kx-ink2">{children}</div>
          <div className="mt-5 flex justify-end gap-2">
            <Button
              variant="secondary"
              onClick={onCancel}
              disabled={busy}
              data-testid="confirm-cancel"
            >
              {cancelLabel}
            </Button>
            <Button variant={tone} onClick={onConfirm} disabled={busy} data-testid="confirm-ok">
              {busy ? "Working…" : confirmLabel}
            </Button>
          </div>
        </div>
      )}
    </dialog>
  );
}
