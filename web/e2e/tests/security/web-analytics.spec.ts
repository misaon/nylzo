import { readFileSync } from 'node:fs';

import type { Page } from '@playwright/test';

import { expect, test } from '../support/fixtures.ts';
import { openApp } from '../support/pages.ts';
import { OFFICIAL_BASE_URL } from '../support/servers.ts';
import { watchViolations } from '../support/watchers.ts';

const OFFICIAL = 'https://nylzo.com';
const spec = readFileSync(new URL('../../../../docs/wiki/reference/Protocol.md', import.meta.url), 'utf8');
const htmlPolicy = /^- HTML responses carry `Content-Security-Policy: ([^`]+)`/mu.exec(spec)?.[1] ?? '';
const [, script = '', endpoint = '', policyName = '', clientHints = '', allowlist = ''] =
  /adds `([^`]+)` to `script-src` and `([^`]+)` to `connect-src`, each after the list above, and `([^`]+)` to `trusted-types`; their `Permissions-Policy` has `([^`]+)` instead of `\(\)`.*?their `Connection-Allowlist` ends with `([^`]+)`/u.exec(
    spec,
  ) ?? [];

// The official instance answers on loopback; the browser sees its pages at nylzo.com, as visitors do.
async function servedAsNylzoCom(page: Page): Promise<string[]> {
  const beaconRequests: string[] = [];
  await page.route(`${OFFICIAL}/**`, async (route) => {
    const { pathname, search } = new URL(route.request().url());
    const response = await route.fetch({
      url: `${OFFICIAL_BASE_URL}${pathname}${search}`,
      headers: { ...route.request().headers(), 'accept-encoding': 'identity' },
    });
    await route.fulfill({ response });
  });
  await page.route(script, async (route) => {
    beaconRequests.push(route.request().url());
    await route.abort();
  });
  return beaconRequests;
}

test(
  'only the home pages of nylzo.com load the pinned Cloudflare beacon, under the policy of spec section 9 (D125)',
  { tag: '@cross-browser' },
  async ({ newPage }) => {
    expect(
      [script, endpoint, policyName, clientHints, allowlist],
      'spec section 9 lost the analytics rule',
    ).not.toContain('');
    const page = await newPage();
    const violations = await watchViolations(page);
    const beaconRequests = await servedAsNylzoCom(page);

    const response = await openApp(page, `${OFFICIAL}/cs/`);
    const policy = (response?.headers()['content-security-policy'] ?? '').replaceAll(/ 'sha384-[^']+'/gu, '');
    expect(policy).toBe(
      htmlPolicy
        .replace(' {script hashes};', ` ${script};`)
        .replace(' {style hashes};', ';')
        .replace("connect-src 'self';", `connect-src 'self' ${endpoint};`)
        .replace('trusted-types nylzo-kdf', `trusted-types nylzo-kdf ${policyName}`),
    );
    expect(response?.headers()['connection-allowlist']?.endsWith(` ${allowlist})`)).toBe(true);
    expect(response?.headers()['permissions-policy']?.split(', ')).toContain(clientHints);
    const beacon = page.locator('script[data-cf-beacon]');
    await expect(beacon).toHaveAttribute('src', script);
    await expect(beacon).toHaveAttribute('type', 'module');
    await expect(beacon).toHaveAttribute('integrity', /^sha384-[A-Za-z0-9+/]{64}$/u);
    await expect(beacon).toHaveAttribute('crossorigin', 'anonymous');
    await expect(beacon).toHaveAttribute('referrerpolicy', 'no-referrer');
    await expect(beacon).toHaveAttribute('fetchpriority', 'low');
    expect(JSON.parse((await beacon.getAttribute('data-cf-beacon')) ?? '{}')).toEqual({
      token: expect.stringMatching(/^[0-9a-f]{32}$/u),
      spa: false,
    });
    await expect.poll(() => beaconRequests).toEqual([script]);
    expect(await violations()).toEqual([]);

    for (const path of ['/sent', '/transparency']) {
      const other = await openApp(page, `${OFFICIAL}${path}`);
      expect(other?.headers()['content-security-policy'], path).not.toContain('cloudflare');
      expect(other?.headers()['permissions-policy'], path).toContain('ch-ua-high-entropy-values=(),');
      await expect(page.locator('script[data-cf-beacon]'), path).toHaveCount(0);
    }
    expect(beaconRequests).toEqual([script]);
    expect(await violations()).toEqual([]);
  },
);
