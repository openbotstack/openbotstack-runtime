import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: '/admin/',
  build: {
    outDir: process.env.OUTDIR || '../../web/webui/admin/dist',
    sourcemap: false,
    assetsInlineLimit: 0,
  },
})
