import { readFileSync } from 'node:fs';

import type { Page } from '@playwright/test';

import { expect, test } from '../support/fixtures.ts';
import { openApp } from '../support/pages.ts';
import { BASE_URL, OFFICIAL_BASE_URL } from '../support/servers.ts';
import { watchViolations } from '../support/watchers.ts';

const OFFICIAL = 'https://nylzo.com';
const spec = readFileSync(new URL('../../../../docs/wiki/reference/Protocol.md', import.meta.url), 'utf8');
const htmlPolicy = /^- HTML responses carry `Content-Security-Policy: ([^`]+)`/mu.exec(spec)?.[1] ?? '';

async function composeMessage(page: Page, path: string): Promise<void> {
  if (path === '/' || path === '/cs/') {
    await page.getByRole('textbox').first().fill('F01 plaintext must stay in this page');
  }
}

for (const origin of [OFFICIAL, BASE_URL]) {
  test(
    `${origin} loads no third-party scripts or analytics while composing a message (F01)`,
    { tag: '@cross-browser' },
    async ({ newPage }) => {
      expect(htmlPolicy, 'spec section 9 lost the HTML policy').not.toBe('');
      const page = await newPage();
      const violations = await watchViolations(page);
      const externalRequests: string[] = [];
      await page.route('**/*', async (route) => {
        const url = new URL(route.request().url());
        if (url.origin !== origin) {
          externalRequests.push(url.href);
          await route.abort();
        } else if (origin === OFFICIAL) {
          // The official instance answers on loopback; the browser sees the production origin and policies.
          const response = await route.fetch({
            url: `${OFFICIAL_BASE_URL}${url.pathname}${url.search}`,
            headers: { ...route.request().headers(), 'accept-encoding': 'identity' },
          });
          await route.fulfill({ response });
        } else {
          await route.continue();
        }
      });

      for (const path of ['/', '/cs/', '/sent', '/transparency']) {
        const response = await openApp(page, `${origin}${path}`);
        const headers = response?.headers() ?? {};
        expect((headers['content-security-policy'] ?? '').replaceAll(/ 'sha384-[^']+'/gu, '')).toBe(
          htmlPolicy.replace(' {script hashes};', ';').replace(' {style hashes};', ';'),
        );
        expect(headers['connection-allowlist']).not.toContain('cloudflare');
        expect(headers['permissions-policy']?.split(', ')).toContain('ch-ua-high-entropy-values=()');
        expect(headers['integrity-policy']).toBe('blocked-destinations=(script)');

        await composeMessage(page, path);
        await expect(page.locator('script[data-cf-beacon]')).toHaveCount(0);
        const sources = await page.locator('script[src]').evaluateAll((scripts) =>
          scripts.map((script) => ({
            origin: new URL(script.getAttribute('src') ?? '', location.href).origin,
            integrity: script.getAttribute('integrity'),
          })),
        );
        expect(sources.length).toBeGreaterThan(0);
        for (const source of sources) {
          expect(source.origin).toBe(origin);
          expect(source.integrity).toMatch(/^sha384-/u);
        }
        expect(externalRequests).toEqual([]);
        expect(await violations()).toEqual([]);
      }
    },
  );
}
