/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: [
    './index.html',
    './src/**/*.{js,ts,jsx,tsx}',
  ],
  theme: {
    extend: {
      colors: {
        border: 'hsl(var(--border))',
        input: 'hsl(var(--input))',
        ring: 'hsl(var(--ring))',
        background: 'hsl(var(--background))',
        foreground: 'hsl(var(--foreground))',
        primary: {
          DEFAULT: 'hsl(var(--primary))',
          foreground: 'hsl(var(--primary-foreground))',
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary))',
          foreground: 'hsl(var(--secondary-foreground))',
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive))',
          foreground: 'hsl(var(--destructive-foreground))',
        },
        muted: {
          DEFAULT: 'hsl(var(--muted))',
          foreground: 'hsl(var(--muted-foreground))',
        },
        accent: {
          DEFAULT: 'hsl(var(--accent))',
          foreground: 'hsl(var(--accent-foreground))',
        },
        popover: {
          DEFAULT: 'hsl(var(--popover))',
          foreground: 'hsl(var(--popover-foreground))',
        },
        card: {
          DEFAULT: 'hsl(var(--card))',
          foreground: 'hsl(var(--card-foreground))',
        },
        /* ── XiaoTianQuant Theme-Aware Palette ──
           深浅色由 .dark class 切换（见 index.css 的 CSS 变量），
           RGB 三元组写法保留 Tailwind 透明度修饰符(/50 等)能力 */
        quant: {
          bg: 'rgb(var(--quant-bg) / <alpha-value>)',
          'bg-secondary': 'rgb(var(--quant-bg-secondary) / <alpha-value>)',
          'bg-tertiary': 'rgb(var(--quant-bg-tertiary) / <alpha-value>)',
          card: 'rgb(var(--quant-card) / <alpha-value>)',
          hover: 'rgb(var(--quant-hover) / <alpha-value>)',
          border: 'rgb(var(--quant-border) / <alpha-value>)',
          'border-light': 'rgb(var(--quant-border-light) / <alpha-value>)',
          gold: '#3699FF',
          'gold-hover': '#5B8DEF',
          green: '#03A66D',
          red: '#CF304A',
          orange: '#F0A030',
        },
      },
      fontFamily: {
        mono: ['"Cascadia Code"', '"Fira Code"', '"JetBrains Mono"', 'monospace'],
        sans: ['-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', 'sans-serif'],
      },
      borderRadius: {
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)',
      },
    },
  },
    animation: {
        'marquee': 'marquee 25s linear infinite',
        'marquee-reverse': 'marquee-reverse 25s linear infinite',
      },
      keyframes: {
        marquee: {
          '0%': { transform: 'translateX(0%)' },
          '100%': { transform: 'translateX(-50%)' },
        },
        'marquee-reverse': {
          '0%': { transform: 'translateX(-50%)' },
          '100%': { transform: 'translateX(0%)' },
        },
      },
  plugins: [require('@tailwindcss/forms')],
}
