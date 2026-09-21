import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

const api = 'http://127.0.0.1:8300';

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 3300,
    strictPort: true,
    proxy: {
      '/api': { target: api, changeOrigin: true },
      '/healthz': { target: api, changeOrigin: true },
    },
  },
  preview: {
    host: '0.0.0.0',
    port: 3300,
    strictPort: true,
    proxy: {
      '/api': { target: api, changeOrigin: true },
      '/healthz': { target: api, changeOrigin: true },
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
});
