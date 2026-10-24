import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { gzipSync } from 'node:zlib';

import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import type { Plugin } from 'vite';
import { defineConfig } from 'vitest/config';

const outDir = resolve(import.meta.dirname, '../../internal/server/web/dist');
const devApi = process.env['NYLZO_DEV_API'] ?? 'http://127.0.0.1:8080';
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

const PRELOADED_FONTS = /^assets\/(?:geist|geist-mono|sora)-latin(?:-ext)?-wght-normal-[\w-]+\.woff2$/;

function preloadFonts(): Plugin {
  return {
    name: 'nylzo-preload-fonts',
    apply: 'build',
    enforce: 'post',
    generateBundle(_, bundle) {
      const index = bundle['index.html'];
      if (index?.type !== 'asset' || typeof index.source !== 'string') {
        return;
      }
      const links = Object.keys(bundle)
        .filter((name) => PRELOADED_FONTS.test(name))
        .sort()
        .map((name) => `<link rel="preload" href="/${name}" as="font" type="font/woff2" crossorigin>`);
      index.source = index.source.replace('</head>', `${links.join('\n')}\n</head>`);
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
  plugins: [tailwindcss(), svelte(), withoutNonceMeta(), preloadFonts(), precompress()],
  html: { cspNonce: '__NYLZO_CSP_NONCE__' },
  server: { strictPort: true, proxy: { '/api': devApi } },
  build: {
    outDir,
    emptyOutDir: true,
    assetsInlineLimit: 0,
    cssCodeSplit: false,
    modulePreload: false,
  },
  worker: { format: 'es' },
  test: {
    projects: [
      { extends: true, test: { name: 'node', include: ['test/**/*.test.ts'], exclude: ['test/**/*.svelte.test.ts'] } },
      {
        extends: true,
        resolve: { conditions: ['browser'] },
        test: {
          name: 'client',
          include: ['test/**/*.svelte.test.ts'],
          environment: './test/client-environment.ts',
        },
      },
    ],
  },
});
