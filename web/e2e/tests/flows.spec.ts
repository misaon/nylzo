import { createViaUi, expect, expectRevealed, openViaUi, test } from './helpers.ts';
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
  await expect(late.getByText('The message has already been opened.')).toBeVisible();
});

test('a wrong passphrase spends no open and the right one reveals the message', async ({ browser }) => {
  const sender = await (await browser.newContext()).newPage();
  const recipient = await (await browser.newContext()).newPage();
  const violations = await recordViolations(recipient);
  const passphrase = 'four random words here';
  const link = await createViaUi(sender, { text: 'chráněno', passphrase, kdf: 'pbkdf2' });
  expect(link).toContain('#1.2.');
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
  const link = await createViaUi(sender, { text: 'scrypt', passphrase, kdf: 'scrypt' });
  expect(link).toContain('#1.1.');
  await openViaUi(recipient, link, passphrase);
  await expectRevealed(recipient);
  await expect(recipient.locator('pre')).toHaveText('scrypt');
  expect(await senderViolations()).toEqual([]);
  expect(await recipientViolations()).toEqual([]);
});
