import { expect, test } from '@playwright/test';

import { recordViolations } from './violations.ts';

const id = 'AAAAAAAAAAAAAAAAAAAAAA';

for (const path of ['/', `/m/${id}`]) {
  test(`${path} renders under the enforced CSP without violations`, async ({ page }) => {
    const violations = await recordViolations(page);
    const response = await page.goto(path);
    const csp = response?.headers()['content-security-policy'] ?? '';
    expect(csp).toContain("require-trusted-types-for 'script'");
    expect(csp).toContain("worker-src 'self'");
    expect(csp).toMatch(/; trusted-types svelte-trusted-html nylzo-kdf$/);
    expect(csp).not.toMatch(/unsafe|data:|blob:/);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    expect(await violations()).toEqual([]);
  });
}

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
