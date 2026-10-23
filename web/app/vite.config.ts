import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [tailwindcss(), svelte()],
  html: { cspNonce: '__NYLZO_CSP_NONCE__' },
  build: {
    outDir: '../../internal/server/web/dist',
    emptyOutDir: true,
    assetsInlineLimit: 0,
    cssCodeSplit: false,
    modulePreload: false,
  },
  worker: { format: 'es' },
});
