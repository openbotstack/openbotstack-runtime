import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: '/ui/',
  build: {
    outDir: process.env.OUTDIR || '../../web/webui/user/dist',
    sourcemap: false,
    assetsInlineLimit: 0,
  },
})
