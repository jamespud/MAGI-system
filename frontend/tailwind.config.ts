import type { Config } from 'tailwindcss';

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        base: 'var(--bg-base)',
        elevated: 'var(--bg-elevated)',
        raised: 'var(--bg-raised)',
        overlay: 'var(--bg-overlay)',
        melchior: 'var(--melchior)',
        balthasar: 'var(--balthasar)',
        casper: 'var(--casper)',
        accent: 'var(--accent)',
        'text-primary': 'var(--text-primary)',
        'text-secondary': 'var(--text-secondary)',
        'text-muted': 'var(--text-muted)',
        'border-dim': 'var(--border-dim)',
      },
      fontFamily: {
        sans: ['IBM Plex Sans', 'Noto Sans SC', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'Noto Sans SC', 'monospace'],
      },
    },
  },
  plugins: [],
} satisfies Config;
