import type { Browser, Page } from '@playwright/test';

import { BASE_URL, createViaApi, createViaUi, expect, expectRevealed, openViaUi, test } from './helpers.ts';
import { recordViolations } from './violations.ts';

declare global {
  interface Window {
    nylzoMarker?: number;
  }
}

async function newPage(browser: Browser): Promise<Page> {
  return (await browser.newContext()).newPage();
}

async function confirm(page: Page, answer: string): Promise<void> {
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: answer, exact: true }).click();
  await expect(dialog).toBeHidden();
}

test('the sender sees the status of a remembered message and cancels it (D39)', async ({ browser }) => {
  const sender = await newPage(browser);
  const violations = await recordViolations(sender);
  const link = await createViaUi(sender, { text: 'cancel me' });
  await expect(sender.getByText('This device remembered the message.')).toBeVisible();
  await sender.getByRole('link', { name: 'Status and cancelling' }).click();
  await expect(sender.getByRole('heading', { name: 'Sent messages' })).toBeVisible();
  const item = sender.getByRole('listitem');
  await expect(item).toHaveCount(1);
  await expect(item.getByText('Waiting to be opened.')).toBeVisible();
  await expect(item.getByText('0 of 1')).toBeVisible();

  await item.getByRole('button', { name: 'Cancel message' }).click();
  await confirm(sender, 'Keep it');
  await expect(item.getByText('Waiting to be opened.')).toBeVisible();
  await item.getByRole('button', { name: 'Cancel message' }).click();
  await confirm(sender, 'Yes, cancel');
  await expect(item.getByText('Cancelled.')).toBeVisible();
  await expect(item.getByRole('button', { name: 'Cancel message' })).toBeDisabled();
  await expect(sender.getByRole('status').first()).toHaveText('The message is cancelled.');

  const recipient = await newPage(browser);
  await recipient.goto(link);
  await expect(recipient.getByText('The sender cancelled the message.')).toBeVisible();
  expect(await violations()).toEqual([]);
});

test('the manage key lies in IndexedDB only as a non-extractable key (D39)', async ({ browser }) => {
  const sender = await newPage(browser);
  const link = await createViaUi(sender, { text: 'key' });
  const id = new URL(link).pathname.slice(3);
  const stored = await sender.evaluate(
    () =>
      new Promise<unknown[]>((resolve, reject) => {
        const opening = indexedDB.open('nylzo');
        opening.onerror = () => reject(opening.error);
        opening.onsuccess = () => {
          const all = opening.result.transaction('sent').objectStore('sent').getAll();
          all.onerror = () => reject(all.error);
          all.onsuccess = async () => {
            const described = [];
            for (const record of all.result as { privateKey: CryptoKey; publicKey: Uint8Array }[]) {
              const exported = await crypto.subtle.exportKey('pkcs8', record.privateKey).then(
                () => 'exported',
                () => 'refused',
              );
              described.push({
                keys: Object.keys(record).sort(),
                id: (record as unknown as { id: string }).id,
                extractable: record.privateKey.extractable,
                algorithm: record.privateKey.algorithm.name,
                usages: record.privateKey.usages,
                publicKey: record.publicKey.length,
                exported,
              });
            }
            resolve(described);
          };
        };
      }),
  );
  expect(stored).toEqual([
    {
      keys: ['createdAt', 'expiresAt', 'id', 'privateKey', 'publicKey'],
      id,
      extractable: false,
      algorithm: 'Ed25519',
      usages: ['sign'],
      publicKey: 32,
      exported: 'refused',
    },
  ]);
});

test('an unchecked box keeps the message off the sent list', async ({ browser }) => {
  const sender = await newPage(browser);
  await sender.goto('/');
  await sender.getByLabel('Remember on this device for status and cancelling').uncheck();
  await sender.getByLabel('Message text').fill('forget me');
  await sender.getByRole('button', { name: 'Create link' }).click();
  await expect(sender.getByLabel('Link to the message')).toBeVisible({ timeout: 45_000 });
  await expect(sender.getByText('This device remembered the message.')).toHaveCount(0);
  await sender.goto('/sent');
  await expect(sender.getByText('This device remembers no messages.')).toBeVisible();
});

test('the sender forgets one message, then all of them, for good', async ({ browser }) => {
  const sender = await newPage(browser);
  for (const text of ['one', 'two', 'three']) {
    await createViaUi(sender, { text });
  }
  await sender.goto('/sent');
  const items = sender.getByRole('listitem');
  await expect(items).toHaveCount(3);
  await expect(items.getByText('Waiting to be opened.')).toHaveCount(3);
  await items.first().getByRole('button', { name: 'Forget' }).click();
  await confirm(sender, 'Yes, forget');
  await expect(items).toHaveCount(2);
  await sender.getByRole('button', { name: 'Forget all' }).click();
  await confirm(sender, 'Yes, forget all');
  await expect(sender.getByText('This device remembers no messages.')).toBeVisible();
  await sender.reload();
  await expect(sender.getByText('This device remembers no messages.')).toBeVisible();
});

test('the recipient deletes a message without showing it (D40)', async ({ browser }) => {
  const message = await createViaApi({ text: 'never shown' });
  const recipient = await newPage(browser);
  const violations = await recordViolations(recipient);
  await recipient.goto(message.link);
  await recipient.getByRole('button', { name: 'Delete without showing' }).click();
  await confirm(recipient, 'Do not delete');
  await expect(recipient.getByRole('button', { name: 'Show message' })).toBeVisible();
  await recipient.getByRole('button', { name: 'Delete without showing' }).click();
  await confirm(recipient, 'Yes, delete');
  await expect(recipient.getByText('The message was deleted.')).toBeVisible();
  expect(await violations()).toEqual([]);

  const late = await newPage(browser);
  await late.goto(message.link);
  await expect(late.getByText('The message was deleted.')).toBeVisible();
});

test('the recipient deletes the remaining opens and keeps reading (D40)', async ({ browser }) => {
  const message = await createViaApi({ text: 'three opens', opensMax: 3 });
  const recipient = await newPage(browser);
  await openViaUi(recipient, message.link);
  await expectRevealed(recipient);
  await recipient.getByRole('button', { name: 'Delete the remaining opens' }).click();
  await confirm(recipient, 'Yes, delete');
  await expect(recipient.getByRole('status').first()).toHaveText('The remaining opens are deleted.');
  await expect(recipient.locator('pre')).toHaveText('three opens');
  await expect(recipient.getByRole('button', { name: 'Delete the remaining opens' })).toBeDisabled();

  const late = await newPage(browser);
  await late.goto(message.link);
  await expect(late.getByText('The message was deleted.')).toBeVisible();
});

test('the last open offers no deletion', async ({ browser }) => {
  const message = await createViaApi({ text: 'one open' });
  const recipient = await newPage(browser);
  await openViaUi(recipient, message.link);
  await expectRevealed(recipient);
  await expect(recipient.getByRole('button', { name: 'Delete the remaining opens' })).toHaveCount(0);
  await expect(recipient.getByRole('button', { name: 'Report this message' })).toBeVisible();
});

test('a report from the read page or from /report spends no open (D41)', async ({ browser }) => {
  const message = await createViaApi({ text: 'reported' });
  const recipient = await newPage(browser);
  const violations = await recordViolations(recipient);
  await recipient.goto(message.link);
  await recipient.getByRole('button', { name: 'Report this message' }).click();
  await recipient.getByLabel('Reason, optional').fill('looks like phishing');
  await recipient.getByRole('button', { name: 'Send report' }).click();
  await expect(recipient.getByText('We received the report.')).toBeVisible();
  await expect(recipient.getByLabel('Reason, optional')).toHaveValue('');

  const reporter = await newPage(browser);
  await reporter.goto('/report');
  await reporter.getByLabel('Link to the message').fill(message.link.replace(BASE_URL, 'http://evil.test'));
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('The link belongs to another service.')).toBeVisible();
  await expect(reporter.getByLabel('Link to the message')).toBeFocused();
  await reporter.getByLabel('Link to the message').fill(`  ${message.link}  `);
  await reporter.getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByText('We received the report.')).toBeVisible();

  await recipient.getByRole('button', { name: 'Show message' }).click();
  await expectRevealed(recipient);
  await expect(recipient.locator('pre')).toHaveText('reported');
  await recipient.getByLabel('Reason, optional').fill('after reading');
  await recipient.getByRole('button', { name: 'Send report' }).click();
  await expect(recipient.getByText('We received the report.')).toBeVisible();
  expect(await violations()).toEqual([]);
});

test('the language switches in place, keeps a shown message and stays chosen', async ({ browser }) => {
  const message = await createViaApi({ text: 'zůstane' });
  const recipient = await newPage(browser);
  await openViaUi(recipient, message.link);
  await expectRevealed(recipient);
  await recipient.evaluate(() => {
    window.nylzoMarker = 1;
  });
  await recipient.getByRole('button', { name: 'Čeština' }).click();
  await expect(recipient.getByRole('heading', { name: 'Obsah zprávy' })).toBeVisible();
  await expect(recipient.locator('pre')).toHaveText('zůstane');
  expect(await recipient.evaluate(() => [window.nylzoMarker, document.documentElement.lang])).toEqual([1, 'cs']);
  await expect(recipient.getByRole('button', { name: 'English' })).toHaveAttribute('lang', 'en');

  await recipient.goto('/');
  await expect(recipient.getByRole('heading', { name: 'Nová zpráva' })).toBeVisible();
  expect(await recipient.evaluate(() => document.documentElement.lang)).toBe('cs');
  await recipient.getByRole('button', { name: 'English' }).click();
  await expect(recipient.getByRole('heading', { name: 'New message' })).toBeVisible();
});
