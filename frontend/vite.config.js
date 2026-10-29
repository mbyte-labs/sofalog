import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte()],
  // Relative base — a self-hosted deployment may sit behind a sub-path proxy,
  // so no absolute /assets/... URL may be baked into the bundle.
  base: './',
  build: {
    outDir: '../server/web/dist',
    // go:embed fails to compile against a missing or empty directory, so the
    // committed stub index.html / .gitkeep must survive every build.
    emptyOutDir: false,
  },
  server: {
    // PORT lets a harness assign a free port when 5173 is taken.
    port: Number(process.env.PORT) || 5173,
    proxy: { '/api': 'http://localhost:8080' },
  },
})
