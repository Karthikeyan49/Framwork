import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { loadEnv } from 'vite'
import { fileURLToPath } from 'node:url'
import { proxyTarget } from './tooling/proxy.ts'

const root = fileURLToPath(new URL('../..', import.meta.url))

export default defineConfig(({ mode }) => {
  const env = { ...loadEnv(mode, root, ''), ...process.env }
  const target = proxyTarget(env.HTTP_ADDR || '127.0.0.1:8080')
  return {
  plugins: [react()],
  envDir: root,
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: { '/api': { target, rewrite: (path) => path.replace(/^\/api/, '') } },
  },
  test: { include: ['src/**/*.test.{ts,tsx}', 'tooling/**/*.test.ts'], environment: 'jsdom', setupFiles: './src/test/setup.ts', restoreMocks: true },
  }
})
