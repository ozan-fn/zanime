import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Alias @ menunjuk src agar komponen dari lintas fitur tidak saling memakai
// path target bermacam-macam.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  // Dev: API/HWS/playlist dan hasil HLS tetap di server Go (same-origin,
  // WebSocket Upgrade ikut diteruskan), supaya alur Vite dev identik dengan
  // build embed.
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/hls': 'http://localhost:8080',
      '/fetch': 'http://localhost:8080',
    },
  },
})
