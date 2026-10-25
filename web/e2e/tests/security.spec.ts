import type { Page, Request } from '@playwright/test';

import { createViaApi, expect, leave, live, openApp, setPassphrase, test } from './helpers.ts';

interface Recorded {
  readonly url: string;
  readonly headers: Record<string, string>;
  readonly body: string;
}

function headersOf(request: Request): Promise<Record<string, string>> {
  const provisional = new Promise<Record<string, string>>((resolve) =>
    setTimeout(() => resolve(request.headers()), 2000),
  );
  return Promise.race([request.allHeaders(), provisional]);
}

function record(page: Page): Promise<Recorded>[] {
  const seen: Promise<Recorded>[] = [];
  page.on('request', (request: Request) => {
    seen.push(
      headersOf(request).then((headers) => ({
        url: request.url(),
        headers,
        body: request.postDataBuffer()?.toString('latin1') ?? '',
      })),
    );
  });
  return seen;
}

function encodings(bytes: Uint8Array): string[] {
  const buffer = Buffer.from(bytes);
  return [
    buffer.toString('hex'),
    buffer.toString('base64'),
    buffer.toString('base64').replace(/=+$/, ''),
    buffer.toString('base64url'),
  ];
}

test('leaving the create page wipes the draft, the passphrase, the file and the created link', async ({ page }) => {
  await openApp(page, '/');
  await page.getByLabel('Message text').fill('wipe me');
  await setPassphrase(page, 'four random words here');
  await page
    .getByLabel(/Attachment, up to/)
    .setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('notes') });
  await leave(page);
  await expect(page.getByLabel('Message text')).toHaveValue('');
  await expect(page.getByLabel('Passphrase, optional')).toHaveValue('');
  expect(
    await page.getByLabel(/Attachment, up to/).evaluate((input: HTMLInputElement) => input.files?.length ?? 0),
  ).toBe(0);

  await page.getByLabel('Message text').fill('created, then left');
  await page.getByRole('button', { name: 'Create link' }).click();
  const link = page.getByLabel('Link to the message');
  await expect(link).toBeVisible({ timeout: 45_000 });
  const key = new URL(await link.inputValue()).hash.slice(5, 20);
  await leave(page);
  await expect(link).toHaveCount(0);
  const stillShown = (secret: string) =>
    document.body.textContent?.includes(secret) === true ||
    [...document.querySelectorAll('input, textarea')].some((field) =>
      (field as HTMLInputElement).value.includes(secret),
    );
  expect(await page.evaluate(stillShown, key)).toBe(false);

  const reloaded = page.waitForEvent('load');
  await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })));
  await reloaded;
  await expect(page.getByLabel('Message text')).toHaveValue('');
});

test('leaving while a message is encrypted wipes it and a later answer neither shows nor remembers it', async ({
  page,
}) => {
  const asked = Promise.withResolvers<void>();
  const answer = Promise.withResolvers<void>();
  await page.route('**/api/v1/messages', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.continue();
      return;
    }
    asked.resolve();
    await answer.promise;
    await route.continue();
  });
  await openApp(page, '/');
  const secret = 'zebracorn marmalade quokka';
  await page.getByLabel('Message text').fill(secret);
  await page.getByRole('button', { name: 'Create link' }).click();
  await asked.promise;
  await leave(page);
  const words = secret.split(' ');
  const visible = () =>
    page.evaluate(
      (parts) => parts.filter((part) => document.body.textContent?.includes(part)).length,
      words.filter((word) => word.length > 4),
    );
  expect(await visible()).toBe(0);
  await expect(page.getByLabel('Message text')).toHaveValue('');

  const answered = page.waitForResponse(
    (response) => response.url().endsWith('/api/v1/messages') && response.request().method() === 'POST',
  );
  answer.resolve();
  expect((await answered).ok()).toBe(true);
  await page.waitForTimeout(1000);
  await expect(live(page, page.getByLabel('Link to the message'))).toHaveCount(0);
  expect(await page.evaluate(async () => (await indexedDB.databases()).some(({ name }) => name === 'nylzo'))).toBe(
    false,
  );
  expect(await visible()).toBe(0);
});

test('an abuse report sends the id, the reason and a signature but never the fragment (AK2, D41)', async ({
  browser,
}) => {
  const message = await createViaApi({ text: 'report me' });
  const recipient = await (await browser.newContext()).newPage();
  const reporter = await (await browser.newContext()).newPage();
  const fromPage = record(recipient);
  const fromReportPage = record(reporter);
  await recipient.goto(message.link);
  await recipient.getByRole('button', { name: 'Report', exact: true }).click();
  await recipient.getByLabel('Reason, optional').fill('spam');
  await recipient.getByRole('button', { name: 'Send report' }).click();
  await expect(recipient.getByRole('status').first()).toHaveText('Report sent');
  await reporter.goto('/report');
  await reporter.getByLabel('Link to the message').fill(message.link);
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('We received the report.')).toBeVisible();

  const fragment = new URL(message.link).hash.slice(1);
  const forbidden = [fragment, ...encodings(Buffer.from(message.secret, 'base64url'))].map((form) =>
    form.toLowerCase(),
  );
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
    expect(Object.keys(JSON.parse(report.body)).sort()).toEqual(
      report.body.includes('reason') ? ['id', 'reason'] : ['id'],
    );
    expect(JSON.parse(report.body).id).toBe(message.idText);
    expect(report.headers['authorization']).toMatch(/^Nylzo-Sig /);
    expect(report.headers['content-type']).toBe('application/json');
  }
});
