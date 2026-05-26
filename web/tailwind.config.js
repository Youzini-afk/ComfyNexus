/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        bg: { DEFAULT: '#0b0d10', soft: '#11141a', card: '#161a22' },
        border: { DEFAULT: '#1f2530' },
        fg: { DEFAULT: '#e8ecf1', muted: '#9aa3b2', subtle: '#6b7280' },
        brand: { DEFAULT: '#7c8cf8', hover: '#94a2ff' },
        ok: '#3ecf8e',
        warn: '#f5a524',
        err: '#f06f6f',
      },
      fontFamily: {
        sans: [
          'Inter',
          '"Noto Sans SC"',
          'system-ui',
          '-apple-system',
          'Segoe UI',
          'Roboto',
          'sans-serif',
        ],
        mono: [
          'JetBrains Mono',
          'ui-monospace',
          'SFMono-Regular',
          'monospace',
        ],
      },
      boxShadow: {
        soft: '0 1px 2px rgba(0,0,0,0.2), 0 4px 16px rgba(0,0,0,0.18)',
      },
    },
  },
  plugins: [],
};
