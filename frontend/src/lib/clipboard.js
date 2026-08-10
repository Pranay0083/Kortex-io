import { toast } from "sonner";

function legacyCopy(text) {
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.top = "-1000px";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  document.body.removeChild(ta);
  return ok;
}

/**
 * Copy text to the clipboard. navigator.clipboard is undefined on insecure origins
 * (plain http on a LAN IP) and can reject on permission errors, so fall back to
 * execCommand. Resolves true on success; shows a toast either way.
 */
export async function copyText(text, { success = "Copied", silent = false } = {}) {
  let ok = false;
  try {
    if (navigator.clipboard?.writeText && window.isSecureContext !== false) {
      await navigator.clipboard.writeText(text);
      ok = true;
    }
  } catch {
    ok = false;
  }
  if (!ok) ok = legacyCopy(text);
  if (!silent) {
    if (ok) toast.success(success);
    else toast.error("Couldn't access the clipboard — copy it manually");
  }
  return ok;
}
