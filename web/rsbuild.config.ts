import { writeFile } from 'node:fs/promises';
import { defineConfig } from '@rsbuild/core';
import { pluginReact } from '@rsbuild/plugin-react';
import { pluginTailwindcss } from '@rsbuild/plugin-tailwindcss';

// Docs: https://rsbuild.rs/config/
export default defineConfig({
  html: {
    title: 'zanime',
  },
  output: {
    // langsung ke folder yang di-embed server Go (//go:embed all:dist)
    distPath: { root: '../internal/web/dist' },
  },
  // rsbuild membersihkan dist tiap build; .gitkeep dikembalikan supaya
  // `go build` tetap jalan saat frontend belum dibangun (go:embed butuh ≥1 file)
  onAfterBuild: async () => {
    await writeFile('../internal/web/dist/.gitkeep', '');
  },
  server: {
    proxy: {
      // dev: frontend di rsbuild, API & streaming tetap di server Go
      '/api': 'http://localhost:8080',
      '/stream': 'http://localhost:8080',
      '/subs': 'http://localhost:8080',
    },
  },
  plugins: [
    pluginReact({
      reactCompiler: true,
    }),
    pluginTailwindcss(),
  ],
});
