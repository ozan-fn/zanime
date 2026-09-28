import { defineConfig } from '@rsbuild/core';
import { pluginPreact } from '@rsbuild/plugin-preact';
import { pluginTailwindcss } from '@rsbuild/plugin-tailwindcss';

// Docs: https://rsbuild.rs/config/
export default defineConfig({
  plugins: [pluginPreact(), pluginTailwindcss()],
  server: {
    // Semua endpoint Go (API, HLS, subtitle, img) hidup di bawah /api/.
    proxy: { '/api': 'http://localhost:8080' },
  },
});
