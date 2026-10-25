import { BASE_URL, expect, openApp, test } from './helpers.ts';

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
