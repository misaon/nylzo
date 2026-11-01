import { createHash } from 'node:crypto';

import { expect, test } from '../support/fixtures.ts';

function digest(body: string): string {
  return `sha384-${createHash('sha384').update(body).digest('base64')}`;
}

const mutations = [
  { name: 'index', pattern: /\/index-[^/]+\.js$/, extra: '\nglobalThis.nylzoTampered = true' },
  { name: 'start', pattern: /\/start-[^/]+\.js$/, extra: '\nglobalThis.nylzoTampered = true' },
  { name: 'style', pattern: /\/style-[^/]+\.css$/, extra: '\nbody{--nylzo-tampered:yes}' },
];
for (const mutation of mutations) {
  const matches = (url: string) => mutation.pattern.test(new URL(url).pathname);

  test(`rejects a modified ${mutation.name} asset before using it`, { tag: '@cross-browser' }, async ({ newPage }) => {
    const page = await newPage();
    await page.route(
      (url) => matches(url.href),
      async (route) => {
        const response = await route.fetch({
          headers: { ...route.request().headers(), 'accept-encoding': 'identity' },
        });
        const original = await response.text();
        await route.fulfill({ response, body: original + mutation.extra });
      },
    );
    const errors: string[] = [];
    page.on('console', (message) => {
      if (message.type() === 'error') {
        errors.push(message.text());
      }
    });
    await page.goto('/sent');
    await expect.poll(() => errors.join('\n')).toMatch(/integrity|digest|hash/i);
    const state = await page.evaluate(() => ({
      script: Reflect.get(globalThis, 'nylzoTampered') as unknown,
      style: getComputedStyle(document.body).getPropertyValue('--nylzo-tampered'),
    }));
    expect(state.script).toBeUndefined();
    expect(state.style).toBe('');
  });
}

test(
  'blocks foreign and injected inline scripts and styles even with valid SRI',
  { tag: '@cross-browser' },
  async ({ newPage }) => {
    const page = await newPage();
    const foreign = 'https://resource-policy.invalid';
    const script = 'globalThis.nylzoForeign = true';
    const css = 'body{--nylzo-foreign:yes}';
    const outbound: string[] = [];
    await page.route(`${foreign}/**`, async (route) => {
      outbound.push(route.request().url());
      const responses = {
        '/probe.js': { body: script, contentType: 'text/javascript' },
        '/dynamic.js': { body: script, contentType: 'text/javascript' },
        '/probe.css': { body: css, contentType: 'text/css' },
      };
      const payload = Reflect.get(responses, new URL(route.request().url()).pathname) as {
        body: string;
        contentType: string;
      };
      await route.fulfill({
        ...payload,
        headers: { 'Access-Control-Allow-Origin': '*' },
      });
    });
    await page.route(
      (url) => url.pathname === '/sent',
      async (route) => {
        const response = await route.fetch({
          headers: { ...route.request().headers(), 'accept-encoding': 'identity' },
        });
        const original = await response.text();
        const injected = `<script src="${foreign}/probe.js" integrity="${digest(script)}" crossorigin="anonymous"></script><link rel="stylesheet" href="${foreign}/probe.css" integrity="${digest(css)}" crossorigin="anonymous"><script>${script}</script><style>${css}</style>`;
        await route.fulfill({ response, body: original.replace('</head>', () => `${injected}</head>`) });
      },
    );
    await page.goto('/sent');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    expect(
      await page.evaluate(async (url) => {
        try {
          await import(url);
          return 'loaded';
        } catch {
          return 'blocked';
        }
      }, `${foreign}/dynamic.js`),
    ).toBe('blocked');
    expect(outbound).toEqual([]);
    expect(await page.evaluate(() => Reflect.get(globalThis, 'nylzoForeign') as unknown)).toBeUndefined();
    expect(await page.evaluate(() => getComputedStyle(document.body).getPropertyValue('--nylzo-foreign'))).toBe('');
  },
);

test('requires SRI metadata before a same-origin script request can leave the browser', async ({ newPage }) => {
  const page = await newPage();
  const outbound: string[] = [];
  await page.route('**/assets/integrity-probe.*', async (route) => {
    outbound.push(route.request().url());
    await route.fulfill({ body: '', contentType: 'text/javascript' });
  });
  await page.route(
    (url) => url.pathname === '/sent',
    async (route) => {
      const response = await route.fetch({ headers: { ...route.request().headers(), 'accept-encoding': 'identity' } });
      const original = await response.text();
      const injected = `<script crossorigin="anonymous" src="/assets/integrity-probe.js"></script>`;
      await route.fulfill({ response, body: original.replace('</head>', () => `${injected}</head>`) });
    },
  );
  const response = await page.goto('/sent');
  expect(response?.headers()['integrity-policy']).toBe('blocked-destinations=(script)');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  expect(outbound).toEqual([]);
});
