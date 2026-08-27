import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import path from 'path';

// Backend proxy target. `make dev` sets VITE_PROXY_TARGET from MAGI_HTTP_PORT;
// standalone `npm run dev` falls back to the default backend port 8080.
const apiProxyTarget = process.env.VITE_PROXY_TARGET || 'http://localhost:8080';

// Dev server port. `make dev` sets VITE_DEV_SERVER_PORT; standalone falls back
// to the default 5173.
const devServerPort = Number(process.env.VITE_DEV_SERVER_PORT || '5173');

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: devServerPort,
    proxy: {
      '/api': {
        target: apiProxyTarget,
        changeOrigin: true,
      },
    },
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
});
