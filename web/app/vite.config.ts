import type { Plugin } from 'vite';

import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { gzipSync } from 'node:zlib';
import { defineConfig } from 'vitest/config';

import { buildInfo } from '../../tools/build-info.mts';

const information = buildInfo();

const outputDirectory = path.resolve(import.meta.dirname, '../../internal/server/web/dist');
const developmentApi = process.env['NYLZO_DEV_API'] ?? 'http://127.0.0.1:8080';
const GZIP_OS_UNKNOWN = 255;
const PRELOADED_FONTS =
  /^assets\/(?:geist|geist-mono|sora)-(?:latin-wght-normal|latin-ext-wght-normal|czech)-[\w-]+\.woff2$/;
const PRECOMPRESSED = /\.(?:js|css)$/;

function buildMetadata(): Plugin {
  return {
    name: 'nylzo-build-metadata',
    apply: 'build',
    generateBundle() {
      // eslint-disable-next-line unicorn/no-this-outside-of-class -- Rollup exposes asset emission through the plugin hook context.
      this.emitFile({ type: 'asset', fileName: 'assets/build-info.json', source: `${JSON.stringify(information)}\n` });
    },
  };
}

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
        .toSorted((first, second) => first.localeCompare(second))
        .map((name) => `<link rel="preload" href="/${name}" as="font" type="font/woff2" crossorigin>`);
      index.source = index.source.replace('</head>', () => `${links.join('\n')}\n</head>`);
    },
  };
}

function precompress(): Plugin {
  return {
    name: 'nylzo-precompress',
    apply: 'build',
    closeBundle() {
      const assets = path.join(outputDirectory, 'assets');
      for (const name of readdirSync(assets)) {
        if (!PRECOMPRESSED.test(name)) {
          continue;
        }
        const compressed = gzipSync(readFileSync(path.join(assets, name)), { level: 9 });
        compressed[9] = GZIP_OS_UNKNOWN;
        writeFileSync(path.join(assets, `${name}.gz`), compressed);
      }
    },
  };
}

export default defineConfig(({ isSsrBuild }) => ({
  plugins: [
    tailwindcss(),
    svelte(),
    ...(isSsrBuild ? [] : [buildMetadata(), withoutNonceMeta(), preloadFonts(), precompress()]),
  ],
  define: { __NYLZO_BUILD_INFO__: JSON.stringify(information) },
  html: { cspNonce: '__NYLZO_CSP_NONCE__' },
  server: { strictPort: true, proxy: { '/api': developmentApi } },
  build: {
    outDir: outputDirectory,
    emptyOutDir: true,
    assetsInlineLimit: 0,
    cssCodeSplit: false,
    modulePreload: false,
  },
  ssr: { noExternal: ['@nylzo/core'] },
  worker: { format: 'es' },
  test: {
    projects: [
      { test: { name: 'node', include: ['src/**/*.test.ts'], exclude: ['src/**/*.svelte.test.ts'] } },
      {
        resolve: { conditions: ['browser'] },
        test: { name: 'client', include: ['src/**/*.svelte.test.ts'], environment: './client-environment.ts' },
      },
    ],
  },
}));
