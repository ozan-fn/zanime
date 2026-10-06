import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

// tipe default-export plugin tidak cocok di TS ini; runtime terverifikasi ok
import compression from 'vite-plugin-compression'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tailwindcss(),
    svelte(),
    // @ts-expect-error lihat catatan import di atas
    compression({ algorithm: 'gzip', ext: '.gz', compressionOptions: { level: 9 }, deleteOriginFile: false }),
  ],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:3000',
    },
  },
})
