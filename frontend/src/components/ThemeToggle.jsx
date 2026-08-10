import { Moon, Sun } from "lucide-react";
import { useTheme } from "@/context/ThemeContext";

/** Sun / moon button that flips between the light and dark theme. */
export default function ThemeToggle({ className = "" }) {
  const { theme, toggle } = useTheme();
  const next = theme === "dark" ? "light" : "dark";
  return (
    <button
      type="button"
      onClick={toggle}
      aria-label={`Switch to ${next} theme`}
      title={`Switch to ${next} theme`}
      data-testid="theme-toggle"
      className={`inline-flex h-8 w-8 items-center justify-center rounded-control border border-kx-line text-kx-muted transition-colors hover:border-kx-line2 hover:text-kx-ink ${className}`}
    >
      {theme === "dark" ? (
        <Sun size={15} strokeWidth={1.75} aria-hidden="true" />
      ) : (
        <Moon size={15} strokeWidth={1.75} aria-hidden="true" />
      )}
    </button>
  );
}
