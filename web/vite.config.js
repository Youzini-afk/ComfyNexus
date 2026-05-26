import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';
// During dev, we proxy /api, /comfy, and /healthz to the Go server on :8080.
export default defineConfig({
    plugins: [react()],
    resolve: {
        alias: { '@': path.resolve(__dirname, './src') },
    },
    build: {
        // Output directly into the Go embed directory so a single `make build`
        // produces a self-contained binary.
        outDir: '../internal/web/dist',
        emptyOutDir: true,
        sourcemap: false,
        chunkSizeWarningLimit: 1500,
    },
    server: {
        port: 5173,
        proxy: {
            '/api': { target: 'http://localhost:8080', changeOrigin: false, ws: true },
            '/comfy': { target: 'http://localhost:8080', changeOrigin: false, ws: true },
            '/healthz': 'http://localhost:8080',
        },
    },
});
