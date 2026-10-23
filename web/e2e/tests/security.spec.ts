import { KDF_PBKDF2 } from '@nylzo/core';
import { expect, test, type Page, type Request } from '@playwright/test';

import { BASE_URL, createViaApi, createViaUi, expectRevealed, openViaUi } from './helpers.ts';

interface Recorded {
  readonly url: string;
  readonly headers: Record<string, string>;
  readonly body: string;
}

function headersOf(request: Request): Promise<Record<string, string>> {
  const provisional = new Promise<Record<string, string>>((resolve) => setTimeout(() => resolve(request.headers()), 2000));
  return Promise.race([request.allHeaders(), provisional]);
}

function record(page: Page): Promise<Recorded>[] {
  const seen: Promise<Recorded>[] = [];
  page.on('request', (request: Request) => {
    seen.push(headersOf(request).then((headers) => ({ url: request.url(), headers, body: request.postDataBuffer()?.toString('latin1') ?? '' })));
  });
  return seen;
}

test('the fragment and the passphrase never leave the browser (AK2)', async ({ browser }) => {
  const sender = await (await browser.newContext()).newPage();
  const recipient = await (await browser.newContext()).newPage();
  const sent = record(sender);
  const received = record(recipient);
  const passphrase = 'four random words here';
  const link = await createViaUi(sender, { text: 'tajné', passphrase, kdf: 'pbkdf2' });
  await openViaUi(recipient, link, passphrase);
  await expectRevealed(recipient);
  const fragment = new URL(link).hash.slice(1);
  const secret = fragment.split('.')[2] ?? '';
  expect(secret).toHaveLength(43);
  const requests = await Promise.all([...sent, ...received]);
  expect(requests.length).toBeGreaterThan(6);
  for (const request of requests) {
    const flat = `${request.url}\n${JSON.stringify(request.headers)}\n${request.body}`;
    expect(flat).not.toContain(secret);
    expect(flat).not.toContain(fragment);
    expect(flat).not.toContain(passphrase);
    expect(request.url).not.toContain('#');
    expect(request.headers['referer']).toBeUndefined();
  }
});

test('loading the page and the metadata spends no open (AK3)', async ({ page }) => {
  const message = await createViaApi();
  await page.goto(message.link);
  await expect(page.getByRole('button', { name: 'Show message' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Show message' })).toBeVisible();
  const status = await message.api.status(message.id, message.manageKey);
  expect(status).toMatchObject({ state: 'available', opensUsed: 0 });
});

test('prefetch and prerender requests get 503 (AK16)', async ({ request }) => {
  const message = await createViaApi();
  for (const path of [`/m/${message.idText}`, '/api/v1/instance']) {
    for (const purpose of ['prefetch', 'prefetch;prerender']) {
      expect((await request.get(path, { headers: { 'Sec-Purpose': purpose } })).status()).toBe(503);
    }
  }
});

test('a speculation rules prerender does not spend an open (AK16)', async ({ page, browserName }) => {
  test.skip(browserName !== 'chromium', 'only Chromium implements speculation rules');
  const message = await createViaApi();
  await page.route(`${BASE_URL}/prerender-host`, (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: `<!doctype html><script type="speculationrules">${JSON.stringify({ prerender: [{ source: 'list', urls: [message.link] }] })}</script><a href="${message.link}">link</a>`,
    }),
  );
  await page.goto(`${BASE_URL}/prerender-host`);
  await page.waitForTimeout(3000);
  const status = await message.api.status(message.id, message.manageKey);
  expect(status).toMatchObject({ state: 'available', opensUsed: 0 });
});

test('the reader stops without a verifier when the server reports another profile (AK24)', async ({ page }) => {
  const passphrase = 'four random words here';
  const message = await createViaApi({ passphrase, kdfId: KDF_PBKDF2 });
  const sensitive: string[] = [];
  page.on('request', (request) => {
    if (request.url().endsWith('/claim') || request.headers()['x-nylzo-verifier']) {
      sensitive.push(request.url());
    }
  });
  await page.route(`${BASE_URL}/api/v1/messages/${message.idText}`, async (route) => {
    const response = await route.fetch();
    const json = (await response.json()) as Record<string, unknown>;
    await route.fulfill({ response, json: { ...json, kdf_id: 1 } });
  });
  await page.goto(message.link);
  await expect(page.getByText(/cannot be opened safely/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Show message' })).toHaveCount(0);
  expect(sensitive).toEqual([]);
  const status = await message.api.status(message.id, message.manageKey);
  expect(status).toMatchObject({ opensUsed: 0, failedPassphraseAttempts: 0 });
});

const files = [
  { name: 'drawing.svg', mimeType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>alert(2)</script></svg>', risky: true },
  { name: 'page.html', mimeType: 'text/html', body: '<!doctype html><script>alert(3)</script>', risky: true },
  { name: 'report.pdf', mimeType: 'application/pdf', body: '%PDF-1.4\n%nylzo\n', risky: false },
];

for (const file of files) {
  test(`an attachment ${file.name} is only ever downloaded (AK13)`, async ({ browser }) => {
    const sender = await (await browser.newContext()).newPage();
    const context = await browser.newContext();
    const recipient = await context.newPage();
    await recipient.addInitScript(() => {
      const types: string[] = [];
      Object.defineProperty(window, 'nylzoBlobTypes', { value: types });
      const original = URL.createObjectURL.bind(URL);
      URL.createObjectURL = (object: Blob | MediaSource) => {
        types.push(object instanceof Blob ? object.type : 'media-source');
        return original(object);
      };
    });
    const link = await createViaUi(sender, { file: { name: file.name, mimeType: file.mimeType, buffer: Buffer.from(file.body) } });
    await openViaUi(recipient, link);
    await expectRevealed(recipient);
    await expect(recipient.getByText(/can run code/)).toHaveCount(file.risky ? 1 : 0);
    const before = recipient.url();
    const [download] = await Promise.all([
      recipient.waitForEvent('download'),
      recipient.getByRole('button', { name: new RegExp(`^Download ${file.name.replace('.', '\\.')}`) }).click(),
    ]);
    expect(download.suggestedFilename()).toBe(file.name);
    expect(recipient.url()).toBe(before);
    expect(context.pages()).toHaveLength(1);
    expect(await recipient.evaluate(() => (window as unknown as { nylzoBlobTypes: string[] }).nylzoBlobTypes)).toEqual(['application/octet-stream']);
  });
}
