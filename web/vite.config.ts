import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  base: '/ui/',  // Embedded at /ui/* in Go binary
  build: {
    outDir: 'webui/dist',
    assetsInlineLimit: 0,  // Don't inline - embed all assets
  },
})
