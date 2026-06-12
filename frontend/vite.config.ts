import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { resolve } from 'path';

export default defineConfig({
  plugins: [react()],
  optimizeDeps: {
    exclude: ['lucide-react'],
    include: ['react', 'react-dom']
  },
  build: {
    outDir: resolve('../web'),
    rollupOptions: {
      // Single-page entry: index.html bootstraps the SPA, which routes
      // /control, /status and /probes client-side from window.location.pathname.
      input: {
        main: resolve(__dirname, 'index.html')
      },
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom'],
          icons: ['lucide-react']
        }
      }
    },
    minify: 'esbuild',
    chunkSizeWarningLimit: 1000
  }
});
