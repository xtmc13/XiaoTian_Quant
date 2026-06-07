/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'
import { visualizer } from 'rollup-plugin-visualizer'
import path from 'path'

export default defineConfig(({ mode }) => ({
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
  plugins: [
    react(),
    // Bundle analyzer — only in analyze mode
    mode === 'analyze' &&
      visualizer({
        open: true,
        gzipSize: true,
        brotliSize: true,
        filename: 'dist/stats.html',
        template: 'treemap',
      }),
  ].filter(Boolean),
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://127.0.0.1:8080',
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: mode === 'analyze',
    rollupOptions: {
      output: {
        manualChunks: {
          // Core React ecosystem — loaded on every page
          'react-vendor': ['react', 'react-dom', 'react-router-dom'],
          // Data fetching — loaded on most pages
          'query': ['@tanstack/react-query'],
          // State management
          'state': ['zustand'],
          // Charts — only loaded on trading/backtest pages
          'echarts': ['echarts'],
          'klinecharts': ['klinecharts', '@klinecharts/pro'],
          'lightweight-charts': ['lightweight-charts'],
          // Code editor — only loaded on indicator IDE
          'codemirror': [
            'codemirror',
            '@codemirror/lang-python',
            '@codemirror/theme-one-dark',
            '@codemirror/commands',
            '@codemirror/state',
            '@codemirror/language',
            '@codemirror/view',
          ],
          // Date utilities
          'date-fns': ['date-fns'],
          // HTTP client
          'axios': ['axios'],
          // UI utilities
          'ui-utils': ['class-variance-authority', 'clsx', 'tailwind-merge'],
        },
      },
    },
    minify: 'terser',
    chunkSizeWarningLimit: 800,
    terserOptions: {
      compress: {
        drop_console: mode !== 'analyze',
        drop_debugger: true,
        passes: 2,
      },
      mangle: {
        safari10: true,
      },
    },
    // Asset optimization
    assetsInlineLimit: 4096,
    cssCodeSplit: true,
    reportCompressedSize: true,
  },
}))
