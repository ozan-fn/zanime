import { defineConfig } from '@rsbuild/core';
import { pluginReact } from '@rsbuild/plugin-react';
import { pluginTailwindcss } from '@rsbuild/plugin-tailwindcss';

// Docs: https://rsbuild.rs/config/
// reactCompiler dijalankan Rust (jsc.transform.reactCompiler di SWC), bukan
// plugin Babel terpisah — lihat https://rsbuild.rs/plugins/list/plugin-react.
// React 19 tidak butuh react-compiler-runtime tambahan.
export default defineConfig({
  plugins: [pluginReact({ reactCompiler: true }), pluginTailwindcss()],
  html: {
    title: 'zanime',
  },
  server: {
    // Semua endpoint Go (API, HLS, subtitle, img) hidup di bawah /api/.
    proxy: { '/api': 'http://localhost:8080' },
  },
});
