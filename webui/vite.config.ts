// vite.config.ts
import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    allowedHosts: ['dev.916337.xyz'],
    proxy: {
      '/api': {
        target: 'http://localhost:5274',
        changeOrigin: true,
      },
      // 本地背景图文件访问（CSS background-image，不带 JWT）
      '/backgrounds': {
        target: 'http://localhost:5274',
        changeOrigin: true,
      },
      '/update': {
        target: 'http://localhost:5274',
        changeOrigin: true,
      },
    },
  },
})
