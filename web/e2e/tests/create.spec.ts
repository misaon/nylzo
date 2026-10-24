import { createViaUi, expect, setPassphrase, test } from './helpers.ts';

const PHONE = { width: 430, height: 932 };

test('the composer forgets the text, the passphrase and the file once the link exists (SEC-8)', async ({ page }) => {
  const link = await createViaUi(page, {
    text: 'forget me',
    passphrase: 'four random words here',
    file: { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('notes') },
  });
  expect(link).toContain('#1.1.');
  await expect(page.getByLabel('Message text')).toHaveValue('');
  await expect(page.getByLabel('Passphrase, optional')).toHaveValue('');
  expect(await page.getByLabel(/Attachment, up to/).evaluate((input: HTMLInputElement) => input.files?.length ?? 0)).toBe(0);

  await page.getByRole('button', { name: 'New message' }).click();
  await expect(page.getByLabel('Message text')).toBeFocused();
  await expect(page.getByLabel('Message text')).toHaveValue('');
  await expect(page.getByText('notes.txt')).toHaveCount(0);
  await expect(page.getByRole('switch', { name: /^Protect with a passphrase/ })).toHaveAttribute('aria-checked', 'false');
});

test('a refused create keeps the message and hands focus back to Create link', async ({ page }) => {
  await page.route('**/api/v1/messages', (route) =>
    route.request().method() === 'POST'
      ? route.fulfill({ status: 429, contentType: 'application/json', body: JSON.stringify({ error: 'rate_limited', retry_after: 30 }) })
      : route.continue(),
  );
  await page.goto('/');
  await page.getByLabel('Message text').fill('try again later');
  const submit = page.getByRole('button', { name: 'Create link' });
  await submit.click();
  await expect(page.getByRole('alert').filter({ hasText: /^Too many messages in a short time\. Try again in / })).toBeVisible({ timeout: 45_000 });
  await expect(submit).toBeEnabled();
  await expect(submit).toBeFocused();
  await expect(page.getByLabel('Message text')).toHaveValue('try again later');

  await page.unroute('**/api/v1/messages');
  await submit.click();
  await expect(page.getByLabel('Link to the message')).toBeVisible({ timeout: 45_000 });
});

test('on a phone the created panel copies the link and starts a new message', async ({ browser }) => {
  const page = await (await browser.newContext({ viewport: PHONE })).newPage();
  await page.addInitScript(() => {
    const copied: string[] = [];
    Object.defineProperty(window, 'nylzoCopied', { value: copied });
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: (text: string) => {
          copied.push(text);
          return Promise.resolve();
        },
      },
    });
  });
  await page.goto('/');
  await page.getByLabel('Message text').fill('phone');
  await setPassphrase(page, 'four random words here');
  await page.getByRole('button', { name: 'Create link' }).click();
  const link = page.getByLabel('Link to the message');
  await expect(link).toBeVisible({ timeout: 45_000 });
  await page.getByRole('button', { name: 'Copy link' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'The link is on the clipboard' })).toHaveCount(1);
  expect(await page.evaluate(() => (window as unknown as { nylzoCopied: string[] }).nylzoCopied)).toEqual([await link.inputValue()]);

  const another = page.getByRole('button', { name: 'New message' });
  const box = await another.boundingBox();
  expect(box?.height).toBeGreaterThanOrEqual(44);
  await another.click();
  await expect(page.getByLabel('Message text')).toBeFocused();
  await expect(page.getByLabel('Message text')).toHaveValue('');
});
