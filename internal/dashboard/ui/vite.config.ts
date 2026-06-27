import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: '/dashboard/',
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') }
  },
  build: { outDir: '../static', emptyOutDir: true },
  server: {
    port: 5173,
    proxy: {
      '/dashboard/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
