/// <reference types="vitest/config" />
import { writeFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

const backend = process.env.AIRVAL_API_URL ?? 'http://localhost:8080'

/** dist/ is wiped on build; restore the tracked .gitkeep so `go:embed web/dist` always has a directory. */
const keepDist = (): Plugin => ({
  name: 'airval-keep-dist',
  apply: 'build',
  closeBundle() {
    writeFileSync(fileURLToPath(new URL('./dist/.gitkeep', import.meta.url)), '')
  },
})

export default defineConfig({
  plugins: [react(), tailwindcss(), keepDist()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    proxy: {
      '/v1': { target: backend, changeOrigin: true },
      '/hooks': { target: backend, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: true,
    chunkSizeWarningLimit: 900,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
