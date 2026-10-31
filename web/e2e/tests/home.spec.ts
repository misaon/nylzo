import { expect, test } from './support/fixtures.ts';
import { openApp } from './support/pages.ts';
import { holdRoute } from './support/routes.ts';
import { BASE_URL } from './support/servers.ts';
import { watchViolations } from './support/watchers.ts';

const HOMES = [
  { path: '/', language: 'en', heading: 'A secret that opens once', title: /^Share passwords and files securely/ },
  { path: '/cs/', language: 'cs', heading: 'Tajemství na jedno otevření', title: /^Bezpečně poslat heslo/ },
];

for (const home of HOMES) {
  test(`${home.path} comes prerendered in ${home.language} with its search and preview tags and shows its content without JavaScript (D82 to D84)`, async ({
    request,
    newPage,
  }) => {
    const response = await request.get(home.path);
    expect(response.status()).toBe(200);
    expect(response.headers()['x-robots-tag']).toBeUndefined();
    const html = await response.text();
    expect(html).toContain(`<html lang="${home.language}">`);
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
    const preview = await request.get(image);
    expect(preview.headers()['content-type']).toBe('image/png');
    const structured = /<script type="application\/ld\+json">([^<]+)<\/script>/.exec(html)?.[1] ?? '';
    const graph = (JSON.parse(structured) as { '@graph': { url: string }[] })['@graph'];
    expect(graph.map((node) => node.url)).toEqual([`${BASE_URL}/`, `${BASE_URL}${home.path}`]);

    const page = await newPage({ javaScriptEnabled: false });
    await page.goto(home.path);
    await expect(page.getByRole('heading', { level: 1, name: home.heading })).toBeVisible();
  });
}

test(
  'the home page shows before the app starts, keeps text typed in the meantime and takes over without an error or a second copy (D83, D86)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const violations = await watchViolations(page);
    const consoleErrors: string[] = [];
    page.on('console', (message) => {
      if (message.type() === 'error') {
        consoleErrors.push(message.text());
      }
    });
    const appStart = await holdRoute(page, /\/assets\/start-[\w-]+\.js$/);
    const ready = page.locator('html[data-ready]');
    const text = page.getByLabel('Text zprávy');
    await page.goto('/cs/');
    await expect(page.getByRole('heading', { level: 1, name: 'Tajemství na jedno otevření' })).toBeVisible();
    await expect(ready).toHaveCount(0);
    await text.fill('napsáno předem');
    appStart.release();
    await expect(ready).toHaveCount(1);
    await expect(text).toHaveValue('napsáno předem');
    await expect(page.locator('main')).toHaveCount(1);
    await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
    await page.getByRole('button', { name: 'Vytvořit odkaz' }).click();
    await expect(page.getByLabel('Odkaz na zprávu')).toBeVisible({ timeout: 45_000 });
    expect(consoleErrors).toEqual([]);
    expect(await violations()).toEqual([]);
  },
);

test('the language switch moves between / and /cs/ in place, a saved choice wins and the browser language does not (D82)', async ({
  newPage,
}) => {
  const page = await newPage({ locale: 'cs-CZ' });
  await openApp(page, '/');
  await expect(page.getByRole('heading', { level: 1, name: 'A secret that opens once' })).toBeVisible();
  await expect(page).toHaveURL(`${BASE_URL}/`);
  await page.evaluate(() => Object.assign(globalThis, { nylzoSamePage: true }));
  await page.getByRole('button', { name: 'Čeština' }).click();
  await expect(page).toHaveURL(`${BASE_URL}/cs/`);
  await expect(page).toHaveTitle(/^Bezpečně poslat heslo/);
  expect(await page.evaluate(() => 'nylzoSamePage' in globalThis)).toBe(true);

  await openApp(page, '/#create');
  await expect(page).toHaveURL(`${BASE_URL}/cs/#create`);
  await expect(page.getByRole('heading', { level: 1, name: 'Tajemství na jedno otevření' })).toBeVisible();
  await page.getByRole('button', { name: 'English' }).click();
  await expect(page).toHaveURL(`${BASE_URL}/#create`);
  await openApp(page, '/cs/');
  await expect(page).toHaveURL(`${BASE_URL}/`);
});

test(
  'English section links and legacy fragments navigate without JavaScript',
  { tag: '@cross-browser' },
  async ({ newPage }) => {
    const page = await newPage({ javaScriptEnabled: false });
    const sections = [
      ['create', 'napsat'],
      ['how-it-works', 'jak'],
      ['security', 'bezpecnost'],
      ['resilience', 'odolnost'],
      ['comparison', 'srovnani'],
      ['promises', 'sliby'],
      ['algorithms', 'algoritmy'],
      ['limits', 'hranice'],
    ] as const;

    for (const [current, previous] of sections) {
      for (const fragment of [current, previous]) {
        await page.goto(`/#${fragment}`);
        await expect(page.locator(`section#${current}`)).toBeInViewport();
      }
    }

    const navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true });
    for (const fragment of ['how-it-works', 'security', 'resilience', 'comparison']) {
      await expect(navigation.locator(`a[href="#${fragment}"]`)).toBeAttached();
    }
  },
);
