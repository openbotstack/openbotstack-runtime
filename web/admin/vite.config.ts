import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: '/admin/',
  build: {
    outDir: process.env.OUTDIR || '../../web/webui/admin/dist',
    // outDir is outside the project root, so Vite won't empty it by default.
    // Force it — otherwise stale hashed assets accumulate and get embedded
    // into the Go binary via go:embed (dead weight).
    emptyOutDir: true,
    sourcemap: false,
    assetsInlineLimit: 0,
  },
})
