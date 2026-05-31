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
        // Remap the zinc + emerald scales the (not-yet-bespoke-reskinned) components
        // still use onto the Living Mesh palette, so nothing renders in the old
        // gray/emerald look. Components reskinned to the design classes don't use
        // these — they read CSS vars — so this only re-tones the remainder.
        zinc: {
          50: '#F4F8F9',
          100: '#E9EFF1', // fg-1
          200: '#D6DEE2',
          300: '#A4B0B8', // fg-2
          400: '#8C98A1',
          500: '#6E7B84', // fg-3
          600: '#4A5560',
          700: '#20272F', // raised-2 / strong border
          800: '#171D25', // raised / border
          850: '#10151B',
          900: '#0E1218', // surface
          950: '#07090C', // base
        },
        emerald: {
          300: '#5BF0D4',
          400: '#2DE2C0', // teal — primary accent
          500: '#2DE2C0',
          600: '#1FAE96',
          700: '#1B8C79',
          950: '#04130F', // text on an accent fill
        },
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
