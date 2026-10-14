import path from 'path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'

// 开发时把 API 与图片路径代理到本地 Go 进程:cookie 同源,无需 CORS。
export default defineConfig({
  plugins: [
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, './src') },
  },
  server: {
    host: '127.0.0.1',
    proxy: {
      '/api': 'http://127.0.0.1:8721',
      '/i': 'http://127.0.0.1:8721',
      '/o': 'http://127.0.0.1:8721',
    },
  },
  build: { outDir: 'dist', emptyOutDir: true },
})
