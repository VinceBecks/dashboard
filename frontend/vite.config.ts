/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  // Haupt-Bundle enthält MUI und die Liste der Lucide-Icon-Namen; die Icons selbst werden einzeln nachgeladen.
  build: { chunkSizeWarningLimit: 800 },
  server: {
    // Im Dev-Modus läuft das Backend lokal (make run im Ordner backend/).
    proxy: { '/api': 'http://localhost:8080' },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['src/test/setup.ts'],
  },
});
