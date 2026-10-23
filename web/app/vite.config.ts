import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { gzipSync } from 'node:zlib';

import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig, type Plugin } from 'vite';

const outDir = resolve(import.meta.dirname, '../../internal/server/web/dist');
const GZIP_OS_UNKNOWN = 255;

function withoutNonceMeta(): Plugin {
  return {
    name: 'nylzo-without-nonce-meta',
    apply: 'build',
    enforce: 'post',
    generateBundle(_, bundle) {
      const index = bundle['index.html'];
      if (index?.type === 'asset' && typeof index.source === 'string') {
        index.source = index.source.replace(/\s*<meta property="csp-nonce"[^>]*>/, '');
      }
    },
  };
}

function precompress(): Plugin {
  return {
    name: 'nylzo-precompress',
    apply: 'build',
    closeBundle() {
      const assets = join(outDir, 'assets');
      for (const name of readdirSync(assets)) {
        if (!/\.(js|css)$/.test(name)) {
          continue;
        }
        const gz = gzipSync(readFileSync(join(assets, name)), { level: 9 });
        gz[9] = GZIP_OS_UNKNOWN;
        writeFileSync(join(assets, `${name}.gz`), gz);
      }
    },
  };
}

export default defineConfig({
  plugins: [tailwindcss(), svelte(), withoutNonceMeta(), precompress()],
  html: { cspNonce: '__NYLZO_CSP_NONCE__' },
  build: {
    outDir,
    emptyOutDir: true,
    assetsInlineLimit: 0,
    cssCodeSplit: false,
    modulePreload: false,
  },
  worker: { format: 'es' },
});
