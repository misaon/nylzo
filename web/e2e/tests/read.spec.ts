import { KDF_NONE } from '@nylzo/core';

import { expect, test } from './support/fixtures.ts';
import { createMessageThroughApi } from './support/messages.ts';
import {
  expectMessageShown,
  leavePage,
  nextFrames,
  notInert,
  openApp,
  returnFromBackForwardCache,
  showMessage,
} from './support/pages.ts';
import { BASE_URL } from './support/servers.ts';

declare global {
  var nylzoShown: boolean | undefined;
}

const PASSPHRASE = 'four random words here';
const LEFT = 'You left the page, so it no longer shows the message.';
const REMOVED = /^The key is no longer in the address bar$/;

test('loading drops the key from the address, and reloading or a return from the back/forward cache spend no open (AK3, D95)', async ({
  page,
}) => {
  const message = await createMessageThroughApi({ text: 'still unread' });
  const show = page.getByRole('button', { name: 'Show message' });
  await page.goto(message.link);
  await expect(show).toBeVisible();
  expect(new URL(page.url()).hash).toBe('');
  await page.reload();
  await expect(page.getByText(REMOVED)).toBeVisible();
  await page.goto(message.link);
  await expect(notInert(page, show)).toBeVisible();
  expect(new URL(page.url()).hash).toBe('');
  await leavePage(page);
  await expect(page.getByText(LEFT)).toBeVisible();
  await returnFromBackForwardCache(page);
  await expect(page.getByText(REMOVED)).toBeVisible();
  expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 0 });
  await page.goto(message.link);
  await notInert(page, show).click();
  await expectMessageShown(page);
  await expect(page.locator('pre')).toHaveText('still unread');
});

test(
  'back and forward after reading bring back neither the fragment nor the message and spend no open',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi({ text: 'only once in this tab', opensMax: 2 });
    const readPage = `${BASE_URL}/m/${message.id}`;
    const removed = page.getByText(REMOVED);
    await openApp(page, '/');
    await showMessage(page, message.link);
    await expectMessageShown(page);
    await expect(page).toHaveURL(readPage);
    await page.goto('/transparency');
    await page.goBack();
    await expect(page).toHaveURL(readPage);
    await expect(removed).toBeVisible();
    await expect(page.locator('pre')).toHaveCount(0);
    await page.goBack();
    await expect(page).toHaveURL(`${BASE_URL}/`);
    await page.goForward();
    await expect(page).toHaveURL(readPage);
    await expect(removed).toBeVisible();
    await expect(page.locator('pre')).toHaveCount(0);
    expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 1 });
  },
);

test('the reader acknowledges the open only once the message is on the screen, and leaving before that keeps the lease (D79)', async ({
  newPage,
}) => {
  const message = await createMessageThroughApi({ text: 'on screen' });
  const page = await newPage({ reducedMotion: 'no-preference' });
  const acknowledgements: { readonly shown: boolean; readonly fragment: string }[] = [];
  await page.route('**/api/v1/messages/*/ack', async (route) => {
    acknowledgements.push(
      await page.evaluate(() => ({
        shown: document.querySelector('pre')?.closest('[inert]') === null,
        fragment: location.hash,
      })),
    );
    await route.continue();
  });
  const claimed = page.waitForResponse((response) => response.url().endsWith('/claim'));
  await showMessage(page, message.link);
  const claim = await claimed;
  expect(claim.status()).toBe(200);
  await leavePage(page);
  await expect(page.getByText(LEFT)).toBeVisible();
  await expect(page.locator('pre')).toHaveCount(0);
  expect(new URL(page.url()).hash).toBe('');
  expect(acknowledgements).toEqual([]);

  await page.reload();
  await expect(page.getByText('You already opened the message on this page')).toBeVisible();
  const acknowledged = page.waitForResponse((response) => response.url().endsWith('/ack'));
  await page.getByRole('button', { name: 'Show message' }).click();
  await expectMessageShown(page);
  await expect(page.locator('pre')).toHaveText('on screen');
  const acknowledgement = await acknowledged;
  expect(acknowledgement.status()).toBe(204);
  expect(acknowledgements).toEqual([{ shown: true, fragment: '' }]);
  expect(await message.status()).toMatchObject({ state: 'opened' });
});

test('a second submit while the message opens claims only once', async ({ page }) => {
  const message = await createMessageThroughApi({ text: 'once', passphrase: PASSPHRASE, opensMax: 2 });
  const claims: string[] = [];
  page.on('request', (request) => {
    if (request.url().endsWith('/claim')) {
      claims.push(request.url());
    }
  });
  await page.goto(message.link);
  const field = page.getByLabel('Passphrase', { exact: true });
  await field.fill(PASSPHRASE);
  await field.evaluate((input: HTMLInputElement) => {
    input.form?.requestSubmit();
    input.form?.requestSubmit();
  });
  await expectMessageShown(page);
  await expect(page.locator('pre')).toHaveText('once');
  await expect(page.getByText('Enter the passphrase from the sender.')).toHaveCount(0);
  expect(claims).toHaveLength(1);
});

test('leaving the page wipes the shown message and closes its dialogs without deleting anything', async ({ page }) => {
  const message = await createMessageThroughApi({ text: 'gone after leaving', opensMax: 2 });
  await showMessage(page, message.link);
  await expectMessageShown(page);
  await expect(page.locator('pre')).toHaveText('gone after leaving');
  await page.getByRole('button', { name: 'Report', exact: true }).click();
  await expect(notInert(page, page.getByRole('dialog'))).toBeVisible();
  await page.getByRole('button', { name: 'Delete the remaining opens' }).click();
  await expect(notInert(page, page.getByRole('alertdialog'))).toBeVisible();
  await leavePage(page);
  await expect(page.getByText(LEFT)).toBeVisible();
  await expect(page.locator('pre')).toHaveCount(0);
  const dialogs = page.getByRole('dialog').or(page.getByRole('alertdialog'));
  await expect(notInert(page, dialogs)).toHaveCount(0);
  await expect(notInert(page, page.getByRole('button', { name: 'Delete the remaining opens' }))).toHaveCount(0);
  await expect(notInert(page, page.getByRole('button', { name: 'Report', exact: true }))).toHaveCount(0);
  expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 1 });
});

test('a prefetch or prerender reaches the read page and the API only as a refused Sec-Purpose request and spends no open (AK16)', async ({
  page,
  request,
}) => {
  const message = await createMessageThroughApi();
  const readPath = `/m/${message.id}`;
  for (const path of [readPath, '/api/v1/instance']) {
    for (const purpose of ['prefetch', 'prefetch;prerender']) {
      const response = await request.get(path, { headers: { 'Sec-Purpose': purpose } });
      expect(response.status(), `${path} ${purpose}`).toBe(503);
    }
  }

  const speculative = page
    .context()
    .waitForEvent('response', (response) => new URL(response.url()).pathname === readPath);
  await page.route(`${BASE_URL}/prerender-host`, (route) =>
    route.fulfill({ contentType: 'text/html', body: `<!doctype html><a href="${message.link}">link</a>` }),
  );
  await page.goto(`${BASE_URL}/prerender-host`);
  await page.unrouteAll();
  await page.evaluate((url) => {
    const rules = document.createElement('script');
    rules.type = 'speculationrules';
    rules.textContent = JSON.stringify({ prerender: [{ source: 'list', urls: [url] }] });
    document.head.append(rules);
  }, message.link);
  const response = await speculative;
  const headers = await response.request().allHeaders();
  expect(headers['sec-purpose']).toMatch(/^prefetch/);
  expect(response.status()).toBe(503);
  expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 0 });
});

const HIDDEN_STATES = {
  hidden: () => {
    Object.defineProperty(Document.prototype, 'visibilityState', {
      configurable: true,
      get: () => (globalThis.nylzoShown === true ? 'visible' : 'hidden'),
    });
  },
  prerendering: () => {
    Object.defineProperty(Document.prototype, 'prerendering', {
      configurable: true,
      get: () => globalThis.nylzoShown !== true,
    });
  },
};

for (const [state, hide] of Object.entries(HIDDEN_STATES)) {
  test(`a ${state} page does not start reading until it is shown (AK16)`, async ({ page }) => {
    const message = await createMessageThroughApi({ passphrase: PASSPHRASE, text: 'shown' });
    const field = page.getByLabel('Passphrase', { exact: true });
    const show = page.getByRole('button', { name: 'Show message' });
    await page.addInitScript(hide);
    await showMessage(page, message.link, PASSPHRASE);
    await nextFrames(page);
    await expect(show).toBeEnabled();
    await expect(notInert(page, field)).toBeEditable();
    await expect(page.getByRole('status')).toHaveText('');
    expect(await message.status()).toMatchObject({ opensUsed: 0, failedPassphraseAttempts: 0 });

    await page.evaluate(() => Object.assign(globalThis, { nylzoShown: true }));
    await field.fill(PASSPHRASE);
    await show.click();
    await expectMessageShown(page);
    await expect(page.locator('pre')).toHaveText('shown');
  });
}

test('the reader stops without a verifier when the server reports another passphrase profile (AK24)', async ({
  page,
}) => {
  const message = await createMessageThroughApi({ passphrase: PASSPHRASE });
  const sensitive: string[] = [];
  page.on('request', (request) => {
    if (request.url().endsWith('/claim') || request.headers()['x-nylzo-verifier'] !== undefined) {
      sensitive.push(request.url());
    }
  });
  await page.route(`${BASE_URL}/api/v1/messages/${message.id}`, async (route) => {
    const response = await route.fetch();
    const json = (await response.json()) as Record<string, unknown>;
    await route.fulfill({ response, json: { ...json, kdf_id: KDF_NONE } });
  });
  await page.goto(message.link);
  await expect(page.getByText(/cannot be opened safely/)).toBeVisible();
  await expect(notInert(page, page.getByRole('button', { name: 'Show message' }))).toHaveCount(0);
  expect(sensitive).toEqual([]);
  expect(await message.status()).toMatchObject({ opensUsed: 0, failedPassphraseAttempts: 0 });
});
