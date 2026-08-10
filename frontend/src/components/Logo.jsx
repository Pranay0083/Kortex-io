import { Link } from "react-router-dom";

/** The Kortex mark: a short link "hop" (two nodes joined by an arc) in the accent hue. */
export const Mark = ({ size = 22, className = "" }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    aria-hidden="true"
    className={`flex-none text-kx-accent ${className}`}
  >
    <rect width="24" height="24" rx="6" fill="currentColor" />
    <path
      d="M7 16.5c1.2-5.2 4.3-8.2 10-9"
      fill="none"
      stroke="rgb(var(--kx-on-accent))"
      strokeWidth="2"
      strokeLinecap="round"
    />
    <circle cx="7" cy="16.5" r="2" fill="rgb(var(--kx-on-accent))" />
    <circle cx="17" cy="7.5" r="2" fill="rgb(var(--kx-on-accent))" />
  </svg>
);

/** Mark + wordmark, linking home. */
export default function Logo({ to = "/", size = 22, className = "", testid }) {
  return (
    <Link
      to={to}
      data-testid={testid}
      aria-label="Kortex home"
      className={`inline-flex items-center gap-2 text-kx-ink ${className}`}
    >
      <Mark size={size} />
      <span className="text-[15px] font-semibold tracking-tight">Kortex</span>
    </Link>
  );
}
