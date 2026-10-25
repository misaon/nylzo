import { KDF_SCRYPT } from '@nylzo/core';
import type { Page } from '@playwright/test';

import { createViaApi, expect, expectRevealed, leave, live, openViaUi, test } from './helpers.ts';

const LEFT = 'You left the page, so it no longer shows the message.';

function confirmation(page: Page) {
  return live(page, page.getByRole('alertdialog'));
}

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

test.describe('with full motion', () => {
  test.use({ reducedMotion: 'no-preference' });

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
});

test('a second submit while the message opens starts nothing new', async ({ page }) => {
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
  await field.evaluate((input: HTMLInputElement) => {
    input.form?.requestSubmit();
    input.form?.requestSubmit();
  });
  await expectRevealed(page);
  await expect(page.locator('pre')).toHaveText('once');
  await expect(page.getByText('Enter the passphrase from the sender.')).toHaveCount(0);
  expect(claims).toHaveLength(1);
  expect(await message.api.status(message.id, message.manageKey)).toMatchObject({ opensUsed: 1 });
});
