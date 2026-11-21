import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// The Go server's default listen address. In development Vite serves the
// frontend on :5173 and proxies /api here, so the browser only ever talks to
// one origin and no CORS configuration is needed locally.
//
// If you change SPLASH_HTTP_ADDR, change this target to match.
const GO_ORIGIN = 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [react()],

  server: {
    port: 5173,
    proxy: {
      '/api': { target: GO_ORIGIN, changeOrigin: true },
    },
  },

  preview: {
    port: 4173,
  },

  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // Source maps ship to production: they are only reachable by someone who
    // already has the bundle, and they make a stack trace from the field
    // readable instead of minified noise.
    sourcemap: true,
    target: 'esnext',
    modulePreload: { polyfill: false },
  },

  test: {
    environment: 'jsdom',
    setupFiles: ['./vitest.setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    restoreMocks: true,
    clearMocks: true,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'lcov'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.{ts,tsx}',
        'src/test/**',
        'src/main.tsx',
        'src/vite-env.d.ts',
      ],
    },
  },
})
