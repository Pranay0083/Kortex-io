/** @type {import('tailwindcss').Config} */
module.exports = {
    darkMode: ["class"],
    content: [
    "./src/**/*.{js,jsx,ts,tsx}",
    "./public/index.html"
  ],
  theme: {
    extend: {
      fontFamily: {
        display: ['Chivo', 'ui-sans-serif', 'system-ui'],
        sans: ['Archivo', 'ui-sans-serif', 'system-ui'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'monospace'],
      },
      borderRadius: {
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)'
      },
      colors: {
        kx: {
          bg: '#050505',
          surface: '#0A0A0A',
          card: '#101010',
          hover: '#161616',
          line: '#222222',
          line2: '#2e2e2e',
          cyan: '#00E5FF',
          green: '#00FF66',
          red: '#FF3B30',
          yellow: '#FFD60A',
          violet: '#8A2BE2',
        },
        background: 'hsl(var(--background))',
        foreground: 'hsl(var(--foreground))',
        card: {
          DEFAULT: 'hsl(var(--card))',
          foreground: 'hsl(var(--card-foreground))'
        },
        popover: {
          DEFAULT: 'hsl(var(--popover))',
          foreground: 'hsl(var(--popover-foreground))'
        },
        primary: {
          DEFAULT: 'hsl(var(--primary))',
          foreground: 'hsl(var(--primary-foreground))'
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary))',
          foreground: 'hsl(var(--secondary-foreground))'
        },
        muted: {
          DEFAULT: 'hsl(var(--muted))',
          foreground: 'hsl(var(--muted-foreground))'
        },
        accent: {
          DEFAULT: 'hsl(var(--accent))',
          foreground: 'hsl(var(--accent-foreground))'
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive))',
          foreground: 'hsl(var(--destructive-foreground))'
        },
        border: 'hsl(var(--border))',
        input: 'hsl(var(--input))',
        ring: 'hsl(var(--ring))',
        chart: {
          '1': 'hsl(var(--chart-1))',
          '2': 'hsl(var(--chart-2))',
          '3': 'hsl(var(--chart-3))',
          '4': 'hsl(var(--chart-4))',
          '5': 'hsl(var(--chart-5))'
        }
      },
      keyframes: {
        'accordion-down': { from: { height: '0' }, to: { height: 'var(--radix-accordion-content-height)' } },
        'accordion-up': { from: { height: 'var(--radix-accordion-content-height)' }, to: { height: '0' } },
        'kx-sweep': { '0%': { transform: 'translateX(-100%)' }, '100%': { transform: 'translateX(400%)' } },
        'kx-flash': { '0%': { backgroundColor: 'rgba(0,229,255,0.16)' }, '100%': { backgroundColor: 'transparent' } },
        'kx-rise': { '0%': { opacity: '0', transform: 'translateY(14px)' }, '100%': { opacity: '1', transform: 'translateY(0)' } },
        'kx-pulse': { '0%,100%': { opacity: '1' }, '50%': { opacity: '0.25' } },
        'kx-scan': { '0%': { transform: 'translateY(-100%)' }, '100%': { transform: 'translateY(1000%)' } },
      },
      animation: {
        'accordion-down': 'accordion-down 0.2s ease-out',
        'accordion-up': 'accordion-up 0.2s ease-out',
        'kx-sweep': 'kx-sweep 2.2s linear infinite',
        'kx-flash': 'kx-flash 1.1s ease-out 1',
        'kx-rise': 'kx-rise 0.5s cubic-bezier(0.16,1,0.3,1) both',
        'kx-pulse': 'kx-pulse 1.6s ease-in-out infinite',
        'kx-scan': 'kx-scan 6s linear infinite',
      }
    }
  },
  plugins: [require("tailwindcss-animate")],
};
