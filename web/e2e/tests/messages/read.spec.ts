import { KDF_NONE } from '@nylzo/core';
import type { Page } from '@playwright/test';

import { expect, test } from '../support/fixtures.ts';
import { createMessageThroughApi } from '../support/messages.ts';
import {
  deleteRestButton,
  expectMessageShown,
  leavePage,
  nextFrames,
  notInert,
  openApp,
  returnFromBackForwardCache,
  showMessage,
} from '../support/pages.ts';
import { BASE_URL } from '../support/servers.ts';

declare global {
  var nylzoShown: boolean | undefined;
}

const PASSPHRASE = 'four random words here';
const LEFT = 'You left the page, so it no longer shows the message.';
const REMOVED = /^The key is no longer in the address bar$/u;

test(
  'pagehide removes decrypted text before the browser can freeze the document',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi({ text: 'clear this before freezing', opensMax: 2 });
    await showMessage(page, message.link);
    await expectMessageShown(page);
    const state = await page.evaluate(() => {
      dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }));
      return { plaintextPresent: document.querySelector('pre') !== null, fragmentPresent: location.hash !== '' };
    });
    expect(state).toEqual({ plaintextPresent: false, fragmentPresent: false });
    expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 1 });
  },
);

test('loading drops the key from the address, and reloading or a return from the back/forward cache spend no open', async ({
  page,
}) => {
  const message = await createMessageThroughApi({ text: 'still unread' });
  const show = page.getByRole('button', { name: 'Show message' });
  await page.goto(message.link);
  await expect(show).toBeVisible();
  expect(new URL(page.url()).hash).toBe('');
  await page.reload();
  await expect(page.getByRole('heading', { name: REMOVED })).toBeVisible();
  await page.goto(message.link);
  await expect(notInert(page, show)).toBeVisible();
  expect(new URL(page.url()).hash).toBe('');
  await leavePage(page);
  await expect(page.getByText(LEFT)).toBeVisible();
  await returnFromBackForwardCache(page);
  await expect(page.getByRole('heading', { name: REMOVED })).toBeVisible();
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
    try {
      const message = await createMessageThroughApi({ text: 'only once in this tab', opensMax: 2 });
      const readPage = `${BASE_URL}/r`;
      const removed = page.getByRole('heading', { name: REMOVED });
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
    } catch (error) {
      const diagnostic = await page.evaluate(() => {
        const heading = [...document.querySelectorAll('h3')].find(
          (element) => element.textContent === 'The key is no longer in the address bar',
        );
        const ancestors = [];
        for (let element: Element | null = heading ?? null; element; element = element.parentElement) {
          const style = getComputedStyle(element);
          const box = element.getBoundingClientRect();
          ancestors.push({
            tag: element.tagName,
            classes: element.className,
            visibility: style.visibility,
            display: style.display,
            opacity: style.opacity,
            x: box.x,
            y: box.y,
            width: box.width,
            height: box.height,
          });
        }
        const [navigation] = performance.getEntriesByType('navigation');
        return {
          fontGateReleased: document.documentElement.dataset['fonts'] !== undefined,
          ready: document.documentElement.dataset['ready'] !== undefined,
          visibility: document.visibilityState,
          fontStatus: document.fonts.status,
          navigation: navigation instanceof PerformanceNavigationTiming ? navigation.type : undefined,
          ancestors,
        };
      });
      await test
        .info()
        .attach('history-layout.json', { body: JSON.stringify(diagnostic), contentType: 'application/json' });
      throw error;
    }
  },
);

test('the reader acknowledges the open only once the message is on the screen, and leaving before that keeps the lease', async ({
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
  await expect(page.getByRole('heading', { name: 'Open the link again to finish' })).toBeVisible();
  await page.goto(message.link);
  await expect(page.getByText('You already opened the message here')).toBeVisible();
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
    input.closest('form')?.requestSubmit();
    input.closest('form')?.requestSubmit();
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
  await deleteRestButton(page).click();
  await expect(notInert(page, page.getByRole('alertdialog'))).toBeVisible();
  await leavePage(page);
  await expect(page.getByText(LEFT)).toBeVisible();
  await expect(page.locator('pre')).toHaveCount(0);
  const dialogs = page.getByRole('dialog').or(page.getByRole('alertdialog'));
  await expect(notInert(page, dialogs)).toHaveCount(0);
  await expect(notInert(page, deleteRestButton(page))).toHaveCount(0);
  expect(await message.status()).toMatchObject({ state: 'available', opensUsed: 1 });
});

test('a prefetch or prerender reaches the read page and the API only as a refused Sec-Purpose request and spends no open', async ({
  page,
  request,
}) => {
  const message = await createMessageThroughApi();
  const readPath = '/r';
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
  expect(headers['sec-purpose']).toMatch(/^prefetch/u);
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
  test(`a ${state} page does not start reading until it is shown`, async ({ page }) => {
    const message = await createMessageThroughApi({ passphrase: PASSPHRASE, text: 'shown' });
    const field = page.getByLabel('Passphrase', { exact: true });
    const show = page.getByRole('button', { name: 'Show message' });
    await page.addInitScript(hide);
    await showMessage(page, message.link, { passphrase: PASSPHRASE });
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

test('the reader stops before the handshake when the server reports another passphrase profile', async ({ page }) => {
  const message = await createMessageThroughApi({ passphrase: PASSPHRASE });
  const sensitive: string[] = [];
  page.on('request', (request) => {
    if (request.url().endsWith('/handshake') || request.url().endsWith('/claim')) {
      sensitive.push(request.url());
    }
  });
  await page.route(`${BASE_URL}/api/v1/messages/${message.id}`, async (route) => {
    const response = await route.fetch();
    const metadata: unknown = await response.json();
    const fields = typeof metadata === 'object' ? metadata : null;
    await route.fulfill({ response, json: { ...fields, kdf_id: KDF_NONE } });
  });
  await page.goto(message.link);
  await expect(page.getByRole('heading', { name: /cannot be opened safely/u })).toBeVisible();
  await expect(notInert(page, page.getByRole('button', { name: 'Show message' }))).toHaveCount(0);
  expect(sensitive).toEqual([]);
  expect(await message.status()).toMatchObject({ opensUsed: 0, failedPassphraseAttempts: 0 });
});

async function pasted(into: Page): Promise<string> {
  await into.setContent('<textarea></textarea>');
  await into.locator('textarea').focus();
  await into.keyboard.press('ControlOrMeta+v');
  return into.locator('textarea').inputValue();
}

test('a hand copy of a message keeps its hidden characters, not their labels', async ({ newPage }) => {
  const text = 'pay \u{202E}0001 tnuocca\npass\u{200B}word \u{3164}x';
  const message = await createMessageThroughApi({ text });
  const page = await newPage();
  const paste = await newPage();
  await showMessage(page, message.link);
  await expectMessageShown(page);
  await expect(page.locator('pre mark')).toHaveCount(3);
  await page.locator('pre').evaluate((pre) => getSelection()?.selectAllChildren(pre));
  await page.keyboard.press('ControlOrMeta+c');
  expect(await pasted(paste)).toBe(text);
  await page.locator('pre').evaluate((pre) => {
    const label = pre.querySelector('mark')?.firstChild;
    if (label) {
      getSelection()?.setBaseAndExtent(pre, 0, label, 3);
    }
  });
  await page.keyboard.press('ControlOrMeta+c');
  expect(await pasted(paste), 'a selection that ends inside a label').toBe('pay \u{202E}');
  await page.locator('pre').evaluate((pre) => {
    const label = pre.querySelector('mark')?.firstChild;
    if (label) {
      getSelection()?.setBaseAndExtent(label, label.textContent?.length ?? 0, pre, pre.childNodes.length);
    }
  });
  await page.keyboard.press('ControlOrMeta+c');
  expect(await pasted(paste), 'a selection that starts after a label').toBe(text.slice('pay \u{202E}'.length));
  await page.locator('pre mark').first().click({ clickCount: 3 });
  await page.keyboard.press('ControlOrMeta+c');
  expect(await pasted(paste), 'a triple click on the first line').toMatch(/^pay \u{202E}0001 tnuocca/u);
});

test(
  'marks hidden and direction-changing characters and asks before copying them',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi({ text: 'pay \u{202E}0001 tnuocca' });
    await showMessage(page, message.link);
    await expectMessageShown(page);
    await expect(page.locator('pre mark')).toHaveText('U+202E');
    await expect(page.getByText('Hidden or direction-changing characters in the text: 1.')).toBeVisible();
    expect(await page.locator('pre').textContent()).not.toContain('\u{202E}');
    await page.getByRole('button', { name: 'Copy message' }).click();
    const confirm = page.getByRole('dialog', { name: 'Copy the hidden characters too?' });
    await expect(confirm.getByText(/A copy keeps them exactly as the sender wrote them/u)).toBeVisible();
    await confirm.getByRole('button', { name: 'Do not copy' }).click();
    await expect(confirm).toHaveCount(0);
  },
);
