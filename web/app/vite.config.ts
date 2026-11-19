import { readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { gzipSync } from 'node:zlib';

import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import type { Plugin } from 'vite';
import { defineConfig } from 'vitest/config';

import { buildInfo } from '../../tools/build-info.mts';

const information = buildInfo();

const outputDirectory = path.resolve(import.meta.dirname, '../../internal/server/web/dist');
const developmentApi = process.env['NYLZO_DEV_API'] ?? 'http://127.0.0.1:8080';
const GZIP_OS_UNKNOWN = 255;
const PRELOADED_FONTS = /^assets\/(?:geist|sora)-(?:latin-wght-normal|latin-ext-wght-normal|czech)-[\w-]+\.woff2$/u;
// Admin text is English or Czech, which need no latin-ext glyph.
const ADMIN_PRELOADED_FONTS = /^assets\/(?:geist|sora)-(?:latin-wght-normal|czech)-[\w-]+\.woff2$/u;
const PRECOMPRESSED = /\.(?:js|css)$/u;
// The oldest browsers the client runs in: Safari from the TLS handshake the deployment profiles require
// (X25519MLKEM768), Chrome, Edge and Firefox from Uint8Array.prototype.toBase64, which has no fallback.
const BROWSER_TARGETS = ['chrome140', 'edge140', 'firefox133', 'safari26', 'ios26'];
const SAFARI_MODULE_INTEGRITY_BUG = 'https://bugs.webkit.org/show_bug.cgi?id=326340';

function buildMetadata(): Plugin {
  return {
    name: 'nylzo-build-metadata',
    apply: 'build',
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'assets/build-info.json', source: `${JSON.stringify(information)}\n` });
    },
  };
}

function preloadFonts(fonts: RegExp): Plugin {
  return {
    name: 'nylzo-preload-fonts',
    apply: 'build',
    enforce: 'post',
    generateBundle(_, bundle) {
      const links = Object.keys(bundle)
        .filter((name) => fonts.test(name))
        .toSorted((first, second) => first.localeCompare(second))
        .map((name) => `<link rel="preload" href="/${name}" as="font" type="font/woff2" crossorigin>`);
      for (const page of Object.values(bundle)) {
        if (page.type === 'asset' && page.fileName.endsWith('.html') && typeof page.source === 'string') {
          page.source = page.source.replace('</head>', () => `${links.join('\n')}\n</head>`);
        }
      }
    },
  };
}

const TRUSTED_TYPES_POLICIES: ReadonlySet<string> = new Set(['nylzo-kdf']);

// The page CSP admits only these policy names, so a chunk that creates another one stops the client from starting.
function onlyApprovedTrustedTypesPolicies(): Plugin {
  return {
    name: 'nylzo-only-approved-trusted-types-policies',
    apply: 'build',
    generateBundle(_, bundle) {
      for (const output of Object.values(bundle)) {
        if (output.type !== 'chunk') {
          continue;
        }
        const mentions = output.code.match(/\bcreatePolicy\b/gu)?.length ?? 0;
        const calls = [...output.code.matchAll(/\bcreatePolicy\(\s*["'`]([^"'`]+)["'`]/gu)];
        for (const [, name = ''] of calls) {
          if (!TRUSTED_TYPES_POLICIES.has(name)) {
            this.error(`${output.fileName} creates the Trusted Types policy ${name}, which the page CSP refuses.`);
          }
        }
        if (mentions !== calls.length) {
          this.error(
            `${output.fileName} reaches createPolicy without a literal policy name, which this check cannot verify.`,
          );
        }
      }
    },
  };
}

function staticImportsOnlyFromEntry(): Plugin {
  return {
    name: 'nylzo-static-imports-only-from-entry',
    apply: 'build',
    generateBundle(_, bundle) {
      const chunks = Object.values(bundle).filter((output) => output.type === 'chunk');
      const entries = new Set(chunks.filter((chunk) => chunk.isEntry).map((chunk) => chunk.fileName));
      for (const chunk of chunks) {
        const unloaded = chunk.imports.filter((name) => chunk.isEntry || !entries.has(name));
        if (unloaded.length > 0) {
          this.error(
            `${chunk.fileName} statically imports ${unloaded.join(', ')}. Under Integrity-Policy, Safari blocks a static import of any chunk that is not loaded yet (${SAFARI_MODULE_INTEGRITY_BUG}), so shared code must stay in the entry chunk.`,
          );
        }
      }
    },
  };
}

function precompress(): Plugin {
  return {
    name: 'nylzo-precompress',
    apply: 'build',
    writeBundle({ dir = outputDirectory }, bundle) {
      for (const name of Object.keys(bundle)) {
        if (!PRECOMPRESSED.test(name)) {
          continue;
        }
        const compressed = gzipSync(readFileSync(path.join(dir, name)), { level: 9 });
        compressed[9] = GZIP_OS_UNKNOWN;
        writeFileSync(path.join(dir, `${name}.gz`), compressed);
      }
    },
  };
}

// The admin page builds in a pass of its own into the same directory, so no public page shares a chunk or a
// stylesheet with it; its scripts and stylesheet carry the admin- prefix.
const ADMIN_MODE = 'admin';

function adminAssetName({ names }: { readonly names: readonly string[] }): string {
  return names.some((name) => name.endsWith('.css'))
    ? 'assets/admin-[name]-[hash][extname]'
    : 'assets/[name]-[hash][extname]';
}

export default defineConfig(({ isSsrBuild, mode }) => ({
  plugins: [
    tailwindcss(),
    svelte(),
    ...(isSsrBuild
      ? []
      : [
          ...(mode === ADMIN_MODE ? [] : [buildMetadata()]),
          preloadFonts(mode === ADMIN_MODE ? ADMIN_PRELOADED_FONTS : PRELOADED_FONTS),
          staticImportsOnlyFromEntry(),
          onlyApprovedTrustedTypesPolicies(),
          precompress(),
        ]),
  ],
  define: { __NYLZO_BUILD_INFO__: JSON.stringify(information) },
  server: { strictPort: true, proxy: { '/api': developmentApi } },
  build: {
    ...(!isSsrBuild && { target: BROWSER_TARGETS }),
    outDir: outputDirectory,
    emptyOutDir: mode !== ADMIN_MODE,
    ...(mode === ADMIN_MODE && {
      rolldownOptions: {
        input: path.resolve(import.meta.dirname, 'admin.html'),
        output: {
          entryFileNames: 'assets/admin-[hash].js',
          chunkFileNames: 'assets/admin-[name]-[hash].js',
          assetFileNames: adminAssetName,
        },
      },
    }),
    assetsInlineLimit: 0,
    cssCodeSplit: false,
    modulePreload: false,
    reportCompressedSize: false,
  },
  ssr: { noExternal: ['@nylzo/core'] },
  worker: { format: 'es' },
  test: {
    coverage: {
      provider: 'istanbul',
      reporter: ['text', 'lcov', 'html'],
      include: ['src/**/*.ts', 'src/**/*.svelte'],
      exclude: ['src/**/*.test.ts', 'src/**/*.d.ts'],
    },
    projects: [
      {
        test: {
          name: 'bun',
          include: ['src/**/*.test.ts', 'scripts/**/*.test.ts'],
          exclude: ['src/**/*.svelte.test.ts'],
          testTimeout: 30_000,
          setupFiles: ['src/shared/language/test-catalogs.ts'],
        },
      },
      {
        resolve: { conditions: ['browser'] },
        test: {
          name: 'client',
          include: ['src/**/*.svelte.test.ts'],
          environment: 'happy-dom',
          setupFiles: ['src/shared/language/test-catalogs.ts'],
        },
      },
    ],
  },
}));
