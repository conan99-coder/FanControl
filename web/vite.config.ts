import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The SPA is built to web/dist and embedded into the Go binary via go:embed.
// base "./" so assets resolve relative to the served index.html.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2020',
    sourcemap: false,
  },
  server: {
    // Dev-only proxy: forward API calls to a locally-running fanctrl binary.
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
})
