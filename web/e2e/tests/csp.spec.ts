import { readFileSync } from 'node:fs';

import { expect, test } from './helpers.ts';
import { recordViolations } from './violations.ts';

const id = 'AAAAAAAAAAAAAAAAAAAAAA';
const spec = readFileSync(new URL('../../../spec/protocol.md', import.meta.url), 'utf8');

function specPolicy(lead: string): string {
  const policy = new RegExp(`^- ${lead} carry \`Content-Security-Policy: ([^\`]+)\``, 'm').exec(spec)?.[1];
  if (!policy) {
    throw new Error(`spec section 9 lost the CSP line for "${lead}"`);
  }
  return policy;
}

const htmlPolicy = specPolicy('HTML responses');
const otherPolicy = specPolicy('Responses other than HTML');

function withoutNonce(policy: string): { readonly policy: string; readonly nonces: string[] } {
  const nonces = [...policy.matchAll(/'nonce-([^']+)'/g)].map((match) => match[1] ?? '');
  return { policy: policy.replaceAll(/'nonce-[^']+'/g, "'nonce-{nonce}'"), nonces };
}

for (const path of ['/', '/sent', '/report', `/m/${id}`]) {
  test(`${path} carries exactly the CSP of spec section 9 and renders without violations`, async ({ page }) => {
    const violations = await recordViolations(page);
    const response = await page.goto(path);
    const { policy, nonces } = withoutNonce(response?.headers()['content-security-policy'] ?? '');
    expect(policy).toBe(htmlPolicy);
    expect(nonces).toHaveLength(2);
    expect(nonces[0]).toBe(nonces[1]);
    expect(nonces[0]?.length ?? 0).toBeGreaterThanOrEqual(22);
    expect(response?.headers()['x-robots-tag']).toBe(path === '/' ? undefined : 'noindex, nofollow');
    await expect(page.locator('meta[name="robots"]')).toHaveCount(0);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    expect(await violations()).toEqual([]);
  });
}

test('responses other than HTML carry the short CSP of spec section 9', async ({ request }) => {
  const html = await (await request.get('/')).text();
  const asset = /src="(\/assets\/[^"]+\.js)"/.exec(html)?.[1] ?? '';
  expect(asset).not.toBe('');
  for (const path of ['/api/v1/instance', asset, '/healthz']) {
    expect((await request.get(path)).headers()['content-security-policy'], path).toBe(otherPolicy);
  }
});

test('the icons are files of the embedded client with the short CSP of spec section 9', async ({ page, request }) => {
  await page.goto('/');
  const hrefs = await page.locator('link[rel="icon"], link[rel="apple-touch-icon"]').evaluateAll((links) => links.map((link) => link.getAttribute('href') ?? ''));
  expect(hrefs).toHaveLength(3);
  for (const href of hrefs) {
    expect(href).toMatch(/^\/assets\/[\w-]+\.(?:svg|png)$/);
    const response = await request.get(href);
    expect(response.status(), href).toBe(200);
    expect(response.headers()['content-type'], href).toBe(href.endsWith('.svg') ? 'image/svg+xml' : 'image/png');
    expect(response.headers()['content-security-policy'], href).toBe(otherPolicy);
  }
});

test('every HTML response carries a fresh nonce', async ({ request }) => {
  const nonces = new Set<string>();
  for (let i = 0; i < 3; i++) {
    const response = await request.get('/');
    const nonce = /'nonce-([^']+)'/.exec(response.headers()['content-security-policy'] ?? '')?.[1];
    expect(nonce).toBeTruthy();
    expect(await response.text()).toContain(`nonce="${nonce}"`);
    nonces.add(nonce ?? '');
  }
  expect(nonces.size).toBe(3);
});
