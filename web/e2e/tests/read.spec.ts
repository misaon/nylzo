import { KDF_SCRYPT } from '@nylzo/core';
import type { Page } from '@playwright/test';

import { createViaApi, createViaUi, expect, expectRevealed, leave, live, openViaUi, test } from './helpers.ts';

const LEFT = 'You left the page, so it no longer shows the message.';

function confirmation(page: Page) {
  return live(page, page.getByRole('alertdialog'));
}

test('the delete confirmation closes with Escape or Do not delete and hands focus back, before and after reading', async ({ page }) => {
  const message = await createViaApi({ text: 'keep me', opensMax: 3 });
  await page.goto(message.link);
  const remove = page.getByRole('button', { name: 'Delete without showing' });
  await remove.click();
  await expect(confirmation(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(confirmation(page)).toHaveCount(0);
  await expect(remove).toBeFocused();

  await page.getByRole('button', { name: 'Show message' }).click();
  await expectRevealed(page);
  const rest = page.getByRole('button', { name: 'Delete the remaining opens' });
  await rest.click();
  await expect(confirmation(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(confirmation(page)).toHaveCount(0);
  await expect(rest).toBeFocused();
  await rest.click();
  await confirmation(page).getByRole('button', { name: 'Do not delete' }).click();
  await expect(confirmation(page)).toHaveCount(0);
  await expect(rest).toBeFocused();
  await expect(page.locator('pre')).toHaveText('keep me');
  expect(await message.api.status(message.id, message.manageKey)).toMatchObject({ state: 'available', opensUsed: 1 });
});

test('leaving the page during the delete confirmation closes it and deletes nothing', async ({ page }) => {
  const message = await createViaApi({ text: 'still here' });
  await page.goto(message.link);
  await page.getByRole('button', { name: 'Delete without showing' }).click();
  await expect(confirmation(page)).toBeVisible();
  await leave(page);
  await expect(page.getByText(LEFT)).toBeVisible();
  await expect(confirmation(page)).toHaveCount(0);
  expect(await message.api.status(message.id, message.manageKey)).toMatchObject({ state: 'available', opensUsed: 0 });
});

test('a refused deletion says so where it can be seen and lets the reader try again', async ({ page }) => {
  const message = await createViaApi({ text: 'refused' });
  await page.route('**/api/v1/messages/*/burn', (route) => route.fulfill({ status: 500, body: 'boom' }));
  await page.goto(message.link);
  const remove = page.getByRole('button', { name: 'Delete without showing' });
  await remove.click();
  await confirmation(page).getByRole('button', { name: 'Yes, delete' }).click();
  const note = page.getByRole('paragraph').filter({ hasText: 'The message could not be deleted. Try again.' });
  await expect(note).toBeVisible();
  await expect(note).toHaveCSS('opacity', '1');
  await expect(remove).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Show message' })).toBeEnabled();
  expect(await message.api.status(message.id, message.manageKey)).toMatchObject({ state: 'available', opensUsed: 0 });
});

test('the risky download prompt cancels back to the button and a double click saves the file once (D35)', async ({ browser }) => {
  const sender = await (await browser.newContext()).newPage();
  const recipient = await (await browser.newContext()).newPage();
  await recipient.addInitScript(() => {
    let saved = 0;
    Object.defineProperty(window, 'nylzoSaved', { get: () => saved });
    const original = URL.createObjectURL.bind(URL);
    URL.createObjectURL = (object: Blob | MediaSource) => {
      saved += 1;
      return original(object);
    };
  });
  const downloads: string[] = [];
  recipient.on('download', (download) => downloads.push(download.suggestedFilename()));
  const link = await createViaUi(sender, { file: { name: 'setup.exe', mimeType: 'application/octet-stream', buffer: Buffer.from('MZ') } });
  await openViaUi(recipient, link);
  await expectRevealed(recipient);
  const download = recipient.getByRole('button', { name: 'Download attachment' });
  const cancel = recipient.getByRole('button', { name: 'Do not download' });
  await download.click();
  await expect(cancel).toBeFocused();
  await cancel.click();
  await expect(recipient.getByText(/can run code/)).toHaveCount(0);
  await expect(download).toBeFocused();
  expect(downloads).toEqual([]);

  const savedCount = () => recipient.evaluate(() => (window as unknown as { nylzoSaved: number }).nylzoSaved);
  await download.click();
  const first = recipient.waitForEvent('download');
  await recipient.getByRole('button', { name: 'Download anyway' }).click();
  expect((await first).suggestedFilename()).toBe('setup.exe');
  await expect(download).toBeFocused();
  expect(await savedCount()).toBe(1);

  await download.click();
  const second = recipient.waitForEvent('download');
  await recipient.getByRole('button', { name: 'Download anyway' }).dblclick();
  expect((await second).suggestedFilename()).toBe('setup.exe');
  await expect(recipient.getByText(/can run code/)).toHaveCount(0);
  expect(await savedCount()).toBe(2);
  expect(downloads).toEqual(['setup.exe', 'setup.exe']);
});

test('the reader acknowledges the open only once the message is on the screen', async ({ page }) => {
  const message = await createViaApi({ text: 'on screen' });
  let shownAtAck: boolean | undefined;
  let fragmentAtAck: string | undefined;
  await page.route('**/api/v1/messages/*/ack', async (route) => {
    [shownAtAck, fragmentAtAck] = await page.evaluate(() => {
      const text = document.querySelector('pre');
      return [text !== null && text.closest('[inert]') === null, location.hash] as const;
    });
    await route.continue();
  });
  const acked = page.waitForResponse((response) => response.url().endsWith('/ack'));
  await openViaUi(page, message.link);
  expect((await acked).status()).toBe(204);
  expect(shownAtAck).toBe(true);
  expect(fragmentAtAck).toBe('');
  await expect(page.locator('pre')).toHaveText('on screen');
});

test('leaving while the message opens neither acknowledges nor loses it', async ({ page }) => {
  const message = await createViaApi({ text: 'resume me' });
  const acks: string[] = [];
  page.on('request', (request) => {
    if (request.url().endsWith('/ack')) {
      acks.push(request.url());
    }
  });
  const claimed = page.waitForResponse((response) => response.url().endsWith('/claim'));
  await openViaUi(page, message.link);
  expect((await claimed).status()).toBe(200);
  await leave(page);
  await expect(page.getByText(LEFT)).toBeVisible();
  await expect(page.locator('pre')).toHaveCount(0);
  expect(acks).toEqual([]);
  expect(new URL(page.url()).hash).not.toBe('');

  await page.reload();
  await expect(page.getByText('You already opened the message on this page')).toBeVisible();
  await page.getByRole('button', { name: 'Show message' }).click();
  await expectRevealed(page);
  await expect(page.locator('pre')).toHaveText('resume me');
  expect(acks).toHaveLength(1);
});

test('a second Enter while the message opens starts nothing new', async ({ page }) => {
  const passphrase = 'four random words here';
  const message = await createViaApi({ text: 'once', passphrase, kdfId: KDF_SCRYPT, opensMax: 2 });
  const claims: string[] = [];
  page.on('request', (request) => {
    if (request.url().endsWith('/claim')) {
      claims.push(request.url());
    }
  });
  await page.goto(message.link);
  const field = page.getByLabel('Passphrase', { exact: true });
  await field.fill(passphrase);
  await field.press('Enter');
  await page.keyboard.press('Enter');
  await expectRevealed(page);
  await expect(page.locator('pre')).toHaveText('once');
  await expect(page.getByText('Enter the passphrase from the sender.')).toHaveCount(0);
  expect(claims).toHaveLength(1);
  expect(await message.api.status(message.id, message.manageKey)).toMatchObject({ opensUsed: 1 });
});
