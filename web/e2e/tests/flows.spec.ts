import { BASE_URL, createViaApi, createViaUi, expect, expectRevealed, openApp, openViaUi, test } from './helpers.ts';
import { recordViolations } from './violations.ts';

test('creates and reads a text message under the enforced CSP (AK10)', async ({ browser }) => {
  const sender = await (await browser.newContext()).newPage();
  const recipient = await (await browser.newContext()).newPage();
  const senderViolations = await recordViolations(sender);
  const recipientViolations = await recordViolations(recipient);
  const link = await createViaUi(sender, { text: 'Ahoj z e2e ✓\nřádek dva' });
  expect(link).toMatch(/^http:\/\/127\.0\.0\.1:8090\/m\/[A-Za-z0-9_-]{22}#1\.0\.[A-Za-z0-9_-]{43}$/);
  await openViaUi(recipient, link);
  await expectRevealed(recipient);
  await expect(recipient.locator('pre')).toHaveText('Ahoj z e2e ✓\nřádek dva');
  expect(new URL(recipient.url()).hash).toBe('');
  expect(await senderViolations()).toEqual([]);
  expect(await recipientViolations()).toEqual([]);

  const late = await (await browser.newContext()).newPage();
  await late.goto(link);
  await expect(late.getByText('The message has already been opened')).toBeVisible();
});

test('a wrong passphrase spends no open and the right one reveals the message', async ({ browser }) => {
  const sender = await (await browser.newContext()).newPage();
  const recipient = await (await browser.newContext()).newPage();
  const violations = await recordViolations(recipient);
  const passphrase = 'four random words here';
  const link = await createViaUi(sender, { text: 'chráněno', passphrase });
  expect(link).toContain('#1.1.');
  await openViaUi(recipient, link, 'definitely the wrong one');
  await expect(recipient.getByText(/The passphrase does not match/)).toBeVisible({ timeout: 30_000 });
  await recipient.getByLabel('Passphrase', { exact: true }).fill(passphrase);
  await recipient.getByRole('button', { name: 'Show message' }).click();
  await expectRevealed(recipient);
  await expect(recipient.locator('pre')).toHaveText('chráněno');
  expect(await violations()).toEqual([]);
});

test('scrypt runs in the worker under the enforced CSP', async ({ browser }) => {
  const sender = await (await browser.newContext()).newPage();
  const recipient = await (await browser.newContext()).newPage();
  const senderViolations = await recordViolations(sender);
  const recipientViolations = await recordViolations(recipient);
  const passphrase = 'correct horse battery staple';
  const link = await createViaUi(sender, { text: 'scrypt', passphrase });
  expect(link).toContain('#1.1.');
  await openViaUi(recipient, link, passphrase);
  await expectRevealed(recipient);
  await expect(recipient.locator('pre')).toHaveText('scrypt');
  expect(await senderViolations()).toEqual([]);
  expect(await recipientViolations()).toEqual([]);
});

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
  await page.getByLabel(/Attachment, up to/).setInputFiles({ name: 'big.pdf', mimeType: 'application/pdf', buffer: Buffer.alloc(4 * 1024 * 1024, 1) });
  await page.getByRole('button', { name: 'Create link' }).dblclick();
  await expect(page.getByLabel('Link to the message')).toBeVisible({ timeout: 45_000 });
  expect(sent).toEqual(['GET /api/v1/instance', 'POST /api/v1/messages']);
});

test('a link whose read key starts with a zero byte opens and can be reported (D66)', async ({ page }) => {
  const message = await createViaApi({ text: 'nula na začátku', opensMax: 2, readKeyStartsWithZero: true });
  await openViaUi(page, message.link);
  await expectRevealed(page);
  await expect(page.locator('pre')).toHaveText('nula na začátku');
  await page.getByRole('button', { name: 'Report', exact: true }).click();
  await page.getByLabel('Reason, optional').fill('zero byte');
  await page.getByRole('button', { name: 'Send report' }).click();
  await expect(page.getByRole('status').first()).toHaveText('Report sent');
});

test('the report page accepts a link whose read key starts with a zero byte (D66)', async ({ page }) => {
  const message = await createViaApi({ text: 'nula', readKeyStartsWithZero: true });
  await page.goto(`${BASE_URL}/report`);
  await page.getByLabel('Link to the message').fill(message.link);
  await page.getByRole('button', { name: 'Send report' }).click();
  await expect(page.getByText('We received the report.')).toBeVisible();
});
