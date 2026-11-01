import { readFileSync } from 'node:fs';

import { expect, test } from '../support/fixtures.ts';
import { openApp } from '../support/pages.ts';
import { watchViolations } from '../support/watchers.ts';

const spec = readFileSync(new URL('../../../../docs/wiki/reference/Protocol.md', import.meta.url), 'utf8');
const htmlPolicy = /^- HTML responses carry `Content-Security-Policy: ([^`]+)`/m.exec(spec)?.[1] ?? '';
const PAGES = ['/', '/cs/', '/sent', '/report', '/transparency', '/r', '/m/AAAAAAAAAAAAAAAAAAAAAA'];
const HASHES = / 'sha384-[A-Za-z0-9+/]+={0,2}'/g;

function withHashPlaceholders(policy: string): string {
  return policy
    .replace(/(script-src 'self')(?: 'sha384-[^']+')*/, '$1 {script hashes}')
    .replace(/(style-src 'self')(?: 'sha384-[^']+')*/, '$1 {style hashes}');
}

for (const path of PAGES) {
  test(
    `the ${path} page renders under exactly the CSP of spec section 9 without a violation (AK10, AK19)`,
    { tag: '@cross-browser' },
    async ({ newPage }) => {
      expect(htmlPolicy, 'spec section 9 lost the CSP line for HTML responses').toContain(
        "script-src 'self' {script hashes}; style-src 'self' {style hashes};",
      );
      const page = await newPage();
      const violations = await watchViolations(page);
      const response = await openApp(page, path);
      const policy = response?.headers()['content-security-policy'] ?? '';
      expect(withHashPlaceholders(policy), path).toBe(htmlPolicy);
      expect(policy.includes("script-src 'self' 'sha384-"), `${path} admits its import map by hash`).toBe(true);
      expect(policy.replaceAll(HASHES, ''), path).not.toMatch(/'nonce-|'strict-dynamic'|'unsafe-/);
      await expect(page.getByRole('heading', { level: 1 }), path).toBeVisible();
      expect(await violations(), path).toEqual([]);
    },
  );
}
