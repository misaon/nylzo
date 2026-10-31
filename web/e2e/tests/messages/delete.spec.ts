import type { Page } from '@playwright/test';

import { expect, test } from '../support/fixtures.ts';
import { createMessageThroughApi } from '../support/messages.ts';
import { expectMessageShown, leavePage, notInert, showMessage } from '../support/pages.ts';
import { holdRoute } from '../support/routes.ts';
import { watchViolations } from '../support/watchers.ts';

declare global {
  var nylzoShownAfterLeaving: boolean | undefined;
}

const LEFT = 'You left the page, so it no longer shows the message.';
const DELETION = '**/api/v1/messages/*/burn';

function confirmation(page: Page) {
  return notInert(page, page.getByRole('alertdialog'));
}

test('the recipient deletes a message without showing it, and a dismissed confirmation deletes nothing (D40)', async ({
  page,
  newPage,
}) => {
  const message = await createMessageThroughApi({ text: 'never shown' });
  const violations = await watchViolations(page);
  await page.goto(message.link);
  const remove = page.getByRole('button', { name: 'Delete without showing' });
  const show = page.getByRole('button', { name: 'Show message' });
  await remove.click();
  await expect(confirmation(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(confirmation(page)).toHaveCount(0);
  await expect(remove).toBeFocused();
  await remove.click();
  await confirmation(page).getByRole('button', { name: 'Do not delete' }).click();
  await expect(confirmation(page)).toHaveCount(0);
  await expect(remove).toBeFocused();
  await expect(show).toBeEnabled();
  expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 0 });

  const deletion = await holdRoute(page, DELETION);
  await remove.click();
  await confirmation(page).getByRole('button', { name: 'Yes, delete' }).click();
  await deletion.requested;
  await expect(show).toBeDisabled();
  await expect(remove).toBeDisabled();
  deletion.release();
  await expect(page.getByText('You deleted the message')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'One-time message' })).toBeFocused();
  expect(await violations()).toEqual([]);

  const late = await newPage();
  await late.goto(message.link);
  await expect(late.getByText('The message was deleted')).toBeVisible();
});

test('the recipient deletes the remaining opens, keeps reading and keeps the message while the language changes (D40)', async ({
  page,
  newPage,
}) => {
  const message = await createMessageThroughApi({ text: 'three opens', opensMax: 3 });
  await showMessage(page, message.link);
  await expectMessageShown(page);
  await page.evaluate(() => Object.assign(globalThis, { nylzoSamePage: true }));
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
  expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 1 });

  await rest.click();
  await confirmation(page).getByRole('button', { name: 'Yes, delete' }).click();
  await expect(page.getByRole('status').first()).toHaveText('The remaining opens are deleted.');
  await expect(page.locator('pre')).toHaveText('three opens');
  await page.getByRole('button', { name: 'Čeština' }).click();
  await expect(page.getByRole('heading', { name: 'Obsah zprávy' })).toBeVisible();
  await expect(page.getByRole('status').first()).toHaveText('Zbývající otevření jsou smazaná.');
  await expect(page.locator('pre')).toHaveText('three opens');
  await expect(page.locator('html')).toHaveAttribute('lang', 'cs');
  await expect(page.getByRole('button', { name: 'English' })).toHaveAttribute('lang', 'en');
  expect(await page.evaluate(() => 'nylzoSamePage' in globalThis)).toBe(true);
  await page.getByRole('button', { name: 'English' }).click();
  await expect(rest).toBeDisabled();

  const late = await newPage();
  await late.goto(message.link);
  await expect(late.getByText('The message was deleted')).toBeVisible();
});

test('a refused deletion says so where the reader can see it and lets them try again', async ({ page }) => {
  const message = await createMessageThroughApi({ text: 'refused' });
  await page.route(DELETION, (route) => route.fulfill({ status: 500, body: 'boom' }));
  await page.goto(message.link);
  const remove = page.getByRole('button', { name: 'Delete without showing' });
  await remove.click();
  await confirmation(page).getByRole('button', { name: 'Yes, delete' }).click();
  const note = page.getByRole('paragraph').filter({ hasText: 'The message could not be deleted. Try again.' });
  await expect(note).toBeVisible();
  await expect(note).toHaveCSS('opacity', '1');
  await expect(remove).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Show message' })).toBeEnabled();
  expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 0 });
});

test('a deletion that answers after the page was left never shows the message again', async ({ page }) => {
  const message = await createMessageThroughApi({ text: 'should stay gone', opensMax: 2 });
  await page.addInitScript(() => {
    let left = false;
    let shown = false;
    addEventListener('pagehide', () => {
      left = true;
    });
    new MutationObserver(() => {
      shown ||= left && document.querySelector('pre') !== null;
    }).observe(document, { childList: true, subtree: true, characterData: true });
    Object.defineProperty(globalThis, 'nylzoShownAfterLeaving', {
      get: () => shown || (left && document.querySelector('pre') !== null),
    });
  });
  await showMessage(page, message.link);
  await expectMessageShown(page);
  const deletion = await holdRoute(page, DELETION);
  await page.getByRole('button', { name: 'Delete the remaining opens' }).click();
  await confirmation(page).getByRole('button', { name: 'Yes, delete' }).click();
  await deletion.requested;
  await leavePage(page);
  await expect(page.getByText(LEFT)).toBeVisible();
  const answered = page.waitForResponse((response) => response.url().endsWith('/burn'));
  deletion.release();
  const response = await answered;
  expect(response.status()).toBe(204);
  await expect(page.getByRole('status').first()).toHaveText('The remaining opens are deleted.');
  await expect(page.locator('pre')).toHaveCount(0);
  await expect(page.getByText(LEFT)).toBeVisible();
  expect(await page.evaluate(() => globalThis.nylzoShownAfterLeaving)).toBe(false);
});
