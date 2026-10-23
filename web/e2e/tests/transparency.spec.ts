import { BASE_URL, expect, test } from './helpers.ts';
import { recordViolations } from './violations.ts';

interface Manifest {
  readonly server: string;
  readonly files: readonly { readonly path: string; readonly sha256: string }[];
}

test('the transparency page shows what the server reports', async ({ page }) => {
  const violations = await recordViolations(page);
  const manifest = (await (await page.request.get(`${BASE_URL}/.well-known/nylzo/client.json`)).json()) as Manifest;
  await page.goto('/transparency');
  await expect(page.getByRole('heading', { name: 'Transparency', level: 2 })).toBeVisible();
  await expect(page.getByText('Nylzo e2e')).toBeVisible();
  await expect(page.getByRole('link', { name: 'abuse@nylzo.test' })).toHaveAttribute('href', 'mailto:abuse@nylzo.test');
  await expect(page.getByText(manifest.server, { exact: true })).toBeVisible();
  expect(manifest.files.length).toBeGreaterThan(1);
  for (const file of manifest.files) {
    await expect(page.getByText(file.sha256, { exact: true })).toBeVisible();
  }
  await expect(page.getByText(`nylzo verify-client --server ${BASE_URL}`)).toBeVisible();
  await expect(page.getByText('for at most 7 days').first()).toBeVisible();
  await expect(page.getByRole('link', { name: 'Transparency' })).toHaveAttribute('aria-current', 'page');
  expect(await violations()).toEqual([]);
});

test('the well-known documents keep the API headers', async ({ request }) => {
  for (const path of ['/.well-known/nylzo/client.json', '/.well-known/nylzo/transparency.json']) {
    const response = await request.get(`${BASE_URL}${path}`);
    expect(response.status(), path).toBe(200);
    expect(response.headers()['content-security-policy'], path).toBe("default-src 'none'; frame-ancestors 'none'");
    expect(response.headers()['cache-control'], path).toBe('no-store');
  }
});
