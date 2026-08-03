/** @type {import('tailwindcss').Config} */

// Every kx colour is a CSS variable holding "R G B" channels, set per theme in
// src/index.css (:root = light, [data-theme="dark"] = dark), so opacity
// modifiers like bg-kx-accent/10 keep working in both themes.
const v = (name) => `rgb(var(--kx-${name}) / <alpha-value>)`;

export default {
  content: ["./index.html", "./src/**/*.{js,jsx}"],
  theme: {
    extend: {
      fontFamily: {
        sans: ['"IBM Plex Sans"', "ui-sans-serif", "system-ui", "sans-serif"],
        display: ['"IBM Plex Sans"', "ui-sans-serif", "system-ui", "sans-serif"],
        mono: ['"IBM Plex Mono"', "ui-monospace", "SFMono-Regular", "monospace"],
      },
      colors: {
        kx: {
          bg: v("bg"),
          surface: v("surface"),
          card: v("card"),
          hover: v("hover"),
          line: v("line"),
          line2: v("line2"),
          ink: v("ink"),
          ink2: v("ink2"),
          muted: v("muted"),
          faint: v("faint"),
          accent: v("accent"),
          "on-accent": v("on-accent"),
          positive: v("positive"),
          negative: v("negative"),
          warning: v("warning"),
        },
      },
      borderRadius: {
        card: "8px",
        control: "6px",
      },
      keyframes: {
        "kx-flash": {
          "0%": { backgroundColor: "rgb(var(--kx-accent) / 0.12)" },
          "100%": { backgroundColor: "transparent" },
        },
        "kx-pulse": { "0%,100%": { opacity: "1" }, "50%": { opacity: "0.35" } },
      },
      animation: {
        "kx-flash": "kx-flash 1.1s ease-out 1",
        "kx-pulse": "kx-pulse 1.6s ease-in-out infinite",
      },
    },
  },
  plugins: [],
};
