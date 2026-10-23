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

function encodings(bytes: Buffer): string[] {
  return [
    bytes.toString('hex'),
    bytes.toString('base64'),
    bytes.toString('base64').replace(/=+$/, ''),
    bytes.toString('base64url'),
  ];
}

test('the fragment and the passphrase never leave the browser in any common encoding (AK2)', async ({ browser }) => {
  const sender = await (await browser.newContext()).newPage();
  const recipient = await (await browser.newContext()).newPage();
  const sent = record(sender);
  const received = record(recipient);
  const passphrase = 'four random words here';
  const link = await createViaUi(sender, { text: 'tajné', passphrase, kdf: 'pbkdf2' });
  await openViaUi(recipient, link, passphrase);
  await expectRevealed(recipient);
  const fragment = new URL(link).hash.slice(1);
  const secret = Buffer.from(fragment.split('.')[2] ?? '', 'base64url');
  expect(secret).toHaveLength(32);
  const forbidden = [
    fragment,
    passphrase,
    encodeURIComponent(passphrase),
    passphrase.replaceAll(' ', '+'),
    ...encodings(secret),
    ...encodings(Buffer.from(passphrase, 'utf8')),
  ].map((form) => form.toLowerCase());
  const requests = await Promise.all([...sent, ...received]);
  expect(requests.length).toBeGreaterThan(6);
  for (const request of requests) {
    const flat = `${request.url}\n${JSON.stringify(request.headers)}\n${request.body}\n${Buffer.from(request.body, 'latin1').toString('hex')}`.toLowerCase();
    for (const form of forbidden) {
      expect(flat, `${request.url} leaks ${form}`).not.toContain(form);
    }
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

const hiddenStates = {
  hidden: () => {
    Object.defineProperty(Document.prototype, 'visibilityState', {
      configurable: true,
      get: () => ((window as unknown as { nylzoShown?: boolean }).nylzoShown ? 'visible' : 'hidden'),
    });
  },
  prerendering: () => {
    Object.defineProperty(Document.prototype, 'prerendering', {
      configurable: true,
      get: () => !(window as unknown as { nylzoShown?: boolean }).nylzoShown,
    });
  },
};

for (const [state, script] of Object.entries(hiddenStates)) {
  test(`a ${state} page does not start reading until it is shown (AK16)`, async ({ page }) => {
    const passphrase = 'four random words here';
    const message = await createViaApi({ passphrase, kdfId: KDF_PBKDF2, text: 'shown' });
    await page.addInitScript(script);
    await openViaUi(page, message.link, passphrase);
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    expect(await page.getByRole('button', { name: 'Show message' }).isVisible()).toBe(true);
    expect(await page.getByRole('status').textContent()).toBe('');
    expect(await message.api.status(message.id, message.manageKey)).toMatchObject({ opensUsed: 0, failedPassphraseAttempts: 0 });
    await page.evaluate(() => Object.assign(window, { nylzoShown: true }));
    await page.getByLabel('Passphrase', { exact: true }).fill(passphrase);
    await page.getByRole('button', { name: 'Show message' }).click();
    await expectRevealed(page);
    await expect(page.locator('pre')).toHaveText('shown');
  });
}

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

test('the passphrase fields give password managers nothing and the message field is not spell checked (D38)', async ({ page }) => {
  const message = await createViaApi({ passphrase: 'four random words here', kdfId: KDF_PBKDF2 });
  await page.goto('/');
  const text = page.getByLabel('Message text');
  await expect(text).toHaveAttribute('spellcheck', 'false');
  await expect(text).toHaveAttribute('autocorrect', 'off');
  await expect(text).toHaveAttribute('autocapitalize', 'off');
  await expect(text).toHaveAttribute('autocomplete', 'off');
  const fields = [page.getByLabel('Passphrase, optional')];
  const reader = await page.context().newPage();
  await reader.goto(message.link);
  fields.push(reader.getByLabel('Passphrase', { exact: true }));
  for (const field of fields) {
    await expect(field).toHaveAttribute('autocomplete', 'off');
    await expect(field).toHaveAttribute('spellcheck', 'false');
    await expect(field).toHaveAttribute('data-1p-ignore', '');
    await expect(field).toHaveAttribute('data-lpignore', 'true');
    await expect(field).toHaveAttribute('data-bwignore', 'true');
  }
  for (const screen of [page, reader]) {
    expect(await screen.locator('[autocomplete*="password"], [autocomplete="username"]').count()).toBe(0);
  }
});

const files = [
  { name: 'drawing.svg', shown: 'drawing.svg', extension: 'svg', mimeType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>alert(2)</script></svg>', risky: true },
  { name: 'page.html', shown: 'page.html', extension: 'html', mimeType: 'text/html', body: '<!doctype html><script>alert(3)</script>', risky: true },
  { name: 'invoice‮fdp.exe', shown: 'invoicefdp.exe', extension: 'exe', mimeType: 'application/pdf', body: 'MZ', risky: true },
  { name: 'report.pdf', shown: 'report.pdf', extension: 'pdf', mimeType: 'application/pdf', body: '%PDF-1.4\n%nylzo\n', risky: false },
];

for (const file of files) {
  test(`an attachment ${file.shown} is only ever downloaded under its sanitised name (AK13, D35)`, async ({ browser }) => {
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
    const downloads: string[] = [];
    recipient.on('download', (download) => downloads.push(download.suggestedFilename()));
    const link = await createViaUi(sender, { file: { name: file.name, mimeType: file.mimeType, buffer: Buffer.from(file.body) } });
    await openViaUi(recipient, link);
    await expectRevealed(recipient);
    await expect(recipient.locator('bdi')).toHaveText(file.shown);
    const before = recipient.url();
    const download = recipient.waitForEvent('download');
    await recipient.getByRole('button', { name: 'Download attachment' }).click();
    if (file.risky) {
      await expect(recipient.getByRole('alert')).toHaveText(/can run code/);
      await expect(recipient.getByText(`File extension: .${file.extension}`)).toBeVisible();
      expect(downloads).toEqual([]);
      await recipient.getByRole('button', { name: 'Download anyway' }).click();
    } else {
      await expect(recipient.getByText(/can run code/)).toHaveCount(0);
    }
    expect((await download).suggestedFilename()).toBe(file.shown);
    expect(downloads).toEqual([file.shown]);
    expect(recipient.url()).toBe(before);
    expect(context.pages()).toHaveLength(1);
    expect(await recipient.evaluate(() => (window as unknown as { nylzoBlobTypes: string[] }).nylzoBlobTypes)).toEqual(['application/octet-stream']);
  });
}

test('an abuse report sends the id, the reason and a signature but never the fragment (AK2, D41)', async ({ browser }) => {
  const message = await createViaApi({ text: 'report me' });
  const recipient = await (await browser.newContext()).newPage();
  const reporter = await (await browser.newContext()).newPage();
  const fromPage = record(recipient);
  const fromReportPage = record(reporter);
  await recipient.goto(message.link);
  await recipient.getByRole('button', { name: 'Report this message' }).click();
  await recipient.getByLabel('Reason, optional').fill('spam');
  await recipient.getByRole('button', { name: 'Send report' }).click();
  await expect(recipient.getByText('We received the report.')).toBeVisible();
  await reporter.goto('/report');
  await reporter.getByLabel('Link to the message').fill(message.link);
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('We received the report.')).toBeVisible();

  const fragment = new URL(message.link).hash.slice(1);
  const forbidden = [fragment, ...encodings(Buffer.from(message.secret, 'base64url'))].map((form) => form.toLowerCase());
  const requests = await Promise.all([...fromPage, ...fromReportPage]);
  const reports = requests.filter((request) => request.url.endsWith('/api/v1/abuse'));
  expect(reports.length).toBeGreaterThanOrEqual(2);
  for (const request of requests) {
    const flat = `${request.url}\n${JSON.stringify(request.headers)}\n${request.body}`.toLowerCase();
    for (const form of forbidden) {
      expect(flat, `${request.url} leaks ${form}`).not.toContain(form);
    }
  }
  for (const report of reports) {
    expect(Object.keys(JSON.parse(report.body)).sort()).toEqual(report.body.includes('reason') ? ['id', 'reason'] : ['id']);
    expect(JSON.parse(report.body).id).toBe(message.idText);
    expect(report.headers['authorization']).toMatch(/^Nylzo-Sig /);
    expect(report.headers['content-type']).toBe('application/json');
  }
});
