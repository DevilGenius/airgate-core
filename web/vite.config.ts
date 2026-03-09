import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom', '@tanstack/react-router', '@tanstack/react-query', 'i18next', 'react-i18next'],
        },
      },
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': 'http://localhost:9517',
      '/v1': 'http://localhost:9517',
      '/plugins': 'http://localhost:9517',
      '/setup/status': 'http://localhost:9517',
      '/setup/test-db': 'http://localhost:9517',
      '/setup/test-redis': 'http://localhost:9517',
      '/setup/install': 'http://localhost:9517',
    },
  },
});
