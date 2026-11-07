import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { gzipSync } from 'node:zlib';

import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import browserslist from 'browserslist';
import type { Plugin } from 'vite';
import { defineConfig } from 'vitest/config';

import { buildInfo } from '../../tools/build-info.mts';

const information = buildInfo();

const outputDirectory = path.resolve(import.meta.dirname, '../../internal/server/web/dist');
const developmentApi = process.env['NYLZO_DEV_API'] ?? 'http://127.0.0.1:8080';
const GZIP_OS_UNKNOWN = 255;
const PRELOADED_FONTS = /^assets\/(?:geist|sora)-(?:latin-wght-normal|latin-ext-wght-normal|czech)-[\w-]+\.woff2$/u;
const PRECOMPRESSED = /\.(?:js|css)$/u;
const VITE_BROWSER_NAMES = new Map([
  ['and_chr', 'chrome'],
  ['and_ff', 'firefox'],
  ['chrome', 'chrome'],
  ['edge', 'edge'],
  ['firefox', 'firefox'],
  ['ios_saf', 'ios'],
  ['safari', 'safari'],
]);
const versionOrder = new Intl.Collator('en', { numeric: true });
const SAFARI_MODULE_INTEGRITY_BUG = 'https://bugs.webkit.org/show_bug.cgi?id=326340';

function browserTargets(): string[] {
  const browsers = browserslist(undefined, { path: import.meta.dirname });
  const minimumVersions = new Map<string, string>();
  for (const browser of browsers) {
    const [name = '', versions = ''] = browser.split(' ', 2);
    const viteName = VITE_BROWSER_NAMES.get(name);
    if (viteName === undefined) {
      throw new Error(`Browserslist selected ${browser}, which has no Vite build target.`);
    }
    const [version = versions] = versions.split('-', 1);
    const minimum = minimumVersions.get(viteName);
    if (minimum === undefined || versionOrder.compare(version, minimum) < 0) {
      minimumVersions.set(viteName, version);
    }
  }
  return Array.from(minimumVersions, ([viteName, version]) => `${viteName}${version}`);
}

function buildMetadata(): Plugin {
  return {
    name: 'nylzo-build-metadata',
    apply: 'build',
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'assets/build-info.json', source: `${JSON.stringify(information)}\n` });
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
        for (const [, name = ''] of output.code.matchAll(/createPolicy\(\s*["'`]([^"'`]+)["'`]/gu)) {
          if (!TRUSTED_TYPES_POLICIES.has(name)) {
            this.error(`${output.fileName} creates the Trusted Types policy ${name}, which the page CSP refuses.`);
          }
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
    writeBundle({ dir = outputDirectory }) {
      const assets = path.join(dir, 'assets');
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
    ...(isSsrBuild
      ? []
      : [
          buildMetadata(),
          preloadFonts(),
          staticImportsOnlyFromEntry(),
          onlyApprovedTrustedTypesPolicies(),
          precompress(),
        ]),
  ],
  define: { __NYLZO_BUILD_INFO__: JSON.stringify(information) },
  server: { strictPort: true, proxy: { '/api': developmentApi } },
  build: {
    ...(!isSsrBuild && { target: browserTargets() }),
    outDir: outputDirectory,
    emptyOutDir: true,
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
          environment: './client-environment.ts',
          setupFiles: ['src/shared/language/test-catalogs.ts'],
        },
      },
    ],
  },
}));
