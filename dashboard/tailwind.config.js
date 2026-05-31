/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        // Lattice design system: IBM Plex Mono (code/metrics/IDs) + Hanken Grotesk (UI/display).
        mono: ['"IBM Plex Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'],
        sans: ['"Hanken Grotesk"', 'ui-sans-serif', 'system-ui', '-apple-system', 'sans-serif'],
        display: ['"Hanken Grotesk"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
      },
      colors: {
        // "Cool Fabric, Warm Life" tokens (mirror src/design/colors_and_type.css).
        void: '#000000',
        base: '#07090C',
        surface: '#0E1218',
        raised: '#171D25',
        'raised-2': '#20272F',
        'fg-1': '#E9EFF1',
        'fg-2': '#A4B0B8',
        'fg-3': '#6E7B84',
        teal: '#2DE2C0',
        blue: '#38BDF8',
        green: '#2FD98A',
        amber: '#F5A623',
        'amber-soft': '#FFC24B',
        ember: '#F2792E',
        signal: { DEFAULT: '#2FD98A', dim: '#1C8A58' },
      },
      keyframes: {
        breathe: {
          '0%, 100%': { opacity: '1', transform: 'scale(1)' },
          '50%': { opacity: '0.45', transform: 'scale(0.82)' },
        },
        risein: {
          '0%': { opacity: '0', transform: 'translateY(8px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
      },
      animation: {
        breathe: 'breathe 2.4s ease-in-out infinite',
        risein: 'risein 0.4s ease-out both',
      },
    },
  },
  plugins: [],
}
