import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  base: './',
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:7400', changeOrigin: true },
      '/ws': { target: 'http://localhost:7400', ws: true, changeOrigin: true },
    },
  },
})
