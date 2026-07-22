import react from '@vitejs/plugin-react';
import wails from '@wailsio/runtime/plugins/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react(), wails('./bindings')],
  server: {
    host: '127.0.0.1',
    port: Number(process.env.WAILS_VITE_PORT) || 5188,
    strictPort: false,
  },
});
