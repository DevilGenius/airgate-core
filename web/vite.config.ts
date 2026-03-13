import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import http from 'node:http';

const BACKEND = 'http://localhost:9517';
const backendUrl = new URL(BACKEND);

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: 'api-key-proxy',
      configureServer(server) {
        // 携带 Bearer token 的请求一律代理到后端（API Key 调用）
        server.middlewares.use((req, res, next) => {
          const auth = req.headers.authorization;
          if (auth && auth.startsWith('Bearer ')) {
            const proxyReq = http.request(
              {
                hostname: backendUrl.hostname,
                port: backendUrl.port,
                path: req.url,
                method: req.method,
                headers: req.headers,
              },
              (proxyRes) => {
                res.writeHead(proxyRes.statusCode ?? 502, proxyRes.headers);
                proxyRes.pipe(res);
              },
            );
            proxyReq.on('error', () => {
              res.writeHead(502);
              res.end('Backend unavailable');
            });
            req.pipe(proxyReq);
            return;
          }
          next();
        });
      },
    },
  ],
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
    host: '0.0.0.0',
    port: 3000,
    watch: {
      usePolling: true,
      interval: 1000,
    },
    proxy: {
      '/api': BACKEND,
      '/plugins': BACKEND,
      '/setup/status': BACKEND,
      '/setup/test-db': BACKEND,
      '/setup/test-redis': BACKEND,
      '/setup/install': BACKEND,
    },
  },
});
