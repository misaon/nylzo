import { BASE_URL, expect, test } from './helpers.ts';
import { recordViolations } from './violations.ts';

const HOMES = [
  { path: '/', lang: 'en', heading: 'A secret that opens once', title: /^Share passwords and files securely/ },
  { path: '/cs/', lang: 'cs', heading: 'Tajemství na jedno otevření', title: /^Bezpečně poslat heslo/ },
] as const;

for (const home of HOMES) {
  test(`${home.path} comes prerendered in ${home.lang} with its search and preview tags (D82 to D84)`, async ({ request }) => {
    const response = await request.get(home.path);
    expect(response.status()).toBe(200);
    expect(response.headers()['x-robots-tag']).toBeUndefined();
    const html = await response.text();
    expect(html).toContain(`<html lang="${home.lang}">`);
    expect(html.match(/<h1[\s>]/g)).toHaveLength(1);
    expect(html.replaceAll(/<[^>]+>/g, '')).toContain(home.heading);
    expect(/<title>([^<]*)<\/title>/.exec(html)?.[1]).toMatch(home.title);
    expect(html).toContain(`<link rel="canonical" href="${BASE_URL}${home.path}">`);
    expect(html).toContain(`<link rel="alternate" hreflang="en" href="${BASE_URL}/">`);
    expect(html).toContain(`<link rel="alternate" hreflang="cs" href="${BASE_URL}/cs/">`);
    expect(html).toContain(`<link rel="alternate" hreflang="x-default" href="${BASE_URL}/">`);
    expect(html).not.toContain('nylzo:public');
    const image = /<meta property="og:image" content="([^"]+)">/.exec(html)?.[1] ?? '';
    expect(image.startsWith(`${BASE_URL}/assets/`)).toBe(true);
    const png = await request.get(image);
    expect(png.headers()['content-type']).toBe('image/png');
    const json = /<script type="application\/ld\+json" nonce="[^"]+">([^<]+)<\/script>/.exec(html)?.[1] ?? '';
    expect((JSON.parse(json) as { '@graph': { url: string }[] })['@graph'].map((node) => node.url)).toEqual([`${BASE_URL}/`, `${BASE_URL}${home.path}`]);
  });

  test(`${home.path} shows its content without JavaScript`, async ({ browser }) => {
    const context = await browser.newContext({ javaScriptEnabled: false });
    const page = await context.newPage();
    await page.goto(home.path);
    await expect(page.getByRole('heading', { level: 1, name: home.heading })).toBeVisible();
    await context.close();
  });
}

test('the browser takes over the prerendered page without errors or a second copy', async ({ page }) => {
  const violations = await recordViolations(page);
  const errors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      errors.push(message.text());
    }
  });
  await page.goto('/cs/');
  await expect(page.locator('html[data-ready]')).toHaveCount(1);
  await expect(page.locator('main')).toHaveCount(1);
  await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
  await page.getByRole('textbox').first().fill('hydrated');
  await expect(page.getByRole('textbox').first()).toHaveValue('hydrated');
  expect(errors).toEqual([]);
  expect(await violations()).toEqual([]);
});

test('the language switch moves between / and /cs/ and the choice stays (D82)', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Čeština' }).click();
  await expect(page).toHaveURL(`${BASE_URL}/cs/`);
  await expect(page).toHaveTitle(/^Bezpečně poslat heslo/);
  await page.goto('/#create');
  await expect(page).toHaveURL(`${BASE_URL}/cs/#create`);
  await expect(page.getByRole('heading', { level: 1, name: 'Tajemství na jedno otevření' })).toBeVisible();
  await page.getByRole('button', { name: 'English' }).click();
  await expect(page).toHaveURL(`${BASE_URL}/#create`);
  await page.goto('/cs/');
  await expect(page).toHaveURL(`${BASE_URL}/`);
});

test('a visitor without a saved choice stays on the address they opened', async ({ browser }) => {
  const context = await browser.newContext({ locale: 'cs-CZ' });
  const page = await context.newPage();
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: 'A secret that opens once' })).toBeVisible();
  await expect(page).toHaveURL(`${BASE_URL}/`);
  await context.close();
});

test('/cs redirects to /cs/, and robots.txt and the sitemap point at both home pages', async ({ request }) => {
  const redirect = await request.get('/cs', { maxRedirects: 0 });
  expect(redirect.status()).toBe(308);
  expect(redirect.headers()['location']).toBe('/cs/');
  expect(await (await request.get('/robots.txt')).text()).toBe(`User-agent: *\nDisallow: /api/\n\nSitemap: ${BASE_URL}/sitemap.xml\n`);
  const sitemap = await request.get('/sitemap.xml');
  expect(sitemap.headers()['content-type']).toBe('application/xml; charset=utf-8');
  const xml = await sitemap.text();
  expect(xml).toContain(`<loc>${BASE_URL}/</loc>`);
  expect(xml).toContain(`<loc>${BASE_URL}/cs/</loc>`);
});
