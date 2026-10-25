import { BASE_URL, createViaApi, expect, expectRevealed, openApp, openViaUi, test } from './helpers.ts';

test('a double click on Create link with an attachment creates one message', async ({ page }) => {
  await openApp(page, `${BASE_URL}/`);
  await expect(page.getByLabel('Message text')).toBeVisible();
  const sent: string[] = [];
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname;
    if (path === '/api/v1/instance' || path === '/api/v1/messages') {
      sent.push(`${request.method()} ${path}`);
    }
  });
  await page.getByLabel('Message text').fill('double');
  await page
    .getByLabel(/Attachment, up to/)
    .setInputFiles({ name: 'big.pdf', mimeType: 'application/pdf', buffer: Buffer.alloc(4 * 1024 * 1024, 1) });
  await page.getByRole('button', { name: 'Create link' }).dblclick();
  await expect(page.getByLabel('Link to the message')).toBeVisible({ timeout: 45_000 });
  expect(sent).toEqual(['GET /api/v1/instance', 'POST /api/v1/messages']);
});

test(
  'a link whose read key starts with a zero byte opens and can be reported (D66)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createViaApi({ text: 'nula na začátku', opensMax: 2, readKeyStartsWithZero: true });
    await openViaUi(page, message.link);
    await expectRevealed(page);
    await expect(page.locator('pre')).toHaveText('nula na začátku');
    await page.getByRole('button', { name: 'Report', exact: true }).click();
    await page.getByLabel('Reason, optional').fill('zero byte');
    await page.getByRole('button', { name: 'Send report' }).click();
    await expect(page.getByRole('status').first()).toHaveText('Report sent');
  },
);

test(
  'the report page accepts a link whose read key starts with a zero byte (D66)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createViaApi({ text: 'nula', readKeyStartsWithZero: true });
    await page.goto(`${BASE_URL}/report`);
    await page.getByLabel('Link to the message').fill(message.link);
    await page.getByRole('button', { name: 'Send report' }).click();
    await expect(page.getByText('We received the report.')).toBeVisible();
  },
);
