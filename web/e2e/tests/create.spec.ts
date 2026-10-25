import type { Page, Request } from '@playwright/test';

import { ApiError } from '../../app/src/shared/api/api-errors.ts';
import { createApi } from '../../app/src/shared/api/api.ts';
import { expect, test } from './support/fixtures.ts';
import { createMessageThroughApi } from './support/messages.ts';
import {
  choosePassphrase,
  createdLink,
  createMessageInPage,
  expectMessageShown,
  leavePage,
  notInert,
  openApp,
  returnFromBackForwardCache,
  showMessage,
} from './support/pages.ts';
import { holdRoute } from './support/routes.ts';
import { POW_BASE_URL } from './support/servers.ts';
import { watchViolations } from './support/watchers.ts';

interface CreateAnswer {
  readonly handled: boolean;
  readonly databaseOpens: number;
}

declare global {
  var nylzoCreateAnswer: CreateAnswer | undefined;
}

const PASSPHRASE = 'four random words here';
const NOTES = { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('notes') };

function isCreate(request: Request): boolean {
  return request.url().endsWith('/api/v1/messages') && request.method() === 'POST';
}

test('the composer forgets the text, the passphrase and the file once the link exists or the page is left (SEC-8)', async ({
  page,
}) => {
  const text = page.getByLabel('Message text');
  const passphrase = page.getByLabel('Passphrase, optional');
  const attachment = page.getByLabel(/Attachment, up to/);
  const attachedFiles = () => attachment.evaluate((input: HTMLInputElement) => input.files?.length ?? 0);

  await openApp(page, '/');
  await text.fill('wipe me');
  await choosePassphrase(page, PASSPHRASE);
  await attachment.setInputFiles(NOTES);
  await leavePage(page);
  await expect(text).toHaveValue('');
  await expect(passphrase).toHaveValue('');
  expect(await attachedFiles()).toBe(0);

  const link = await createMessageInPage(page, { text: 'forget me', passphrase: PASSPHRASE, file: NOTES });
  expect(link).toContain('#1.1.');
  await expect(text).toHaveValue('');
  await expect(passphrase).toHaveValue('');
  expect(await attachedFiles()).toBe(0);

  await leavePage(page);
  await expect(page.getByLabel('Link to the message')).toHaveCount(0);
  const secret = link.split('.').at(-1) ?? '';
  const shown = await page.evaluate(
    (part) =>
      document.body.textContent.includes(part) ||
      [...document.querySelectorAll<HTMLInputElement>('input, textarea')].some((field) => field.value.includes(part)),
    secret,
  );
  expect(shown).toBe(false);
  await returnFromBackForwardCache(page);
  await expect(text).toHaveValue('');
});

test('a refused create keeps the message and hands focus back to Create link', async ({ page }) => {
  await page.route('**/api/v1/messages', (route) =>
    isCreate(route.request())
      ? route.fulfill({ status: 429, json: { error: 'rate_limited', retry_after: 30 } })
      : route.continue(),
  );
  await openApp(page, '/');
  await page.getByLabel('Message text').fill('try again later');
  const submit = page.getByRole('button', { name: 'Create link' });
  await submit.click();
  await expect(
    page.getByRole('alert').filter({ hasText: /^Too many messages in a short time\. Try again in / }),
  ).toBeVisible({ timeout: 45_000 });
  await expect(submit).toBeEnabled();
  await expect(submit).toBeFocused();
  await expect(page.getByLabel('Message text')).toHaveValue('try again later');

  await page.unrouteAll();
  await submit.click();
  await createdLink(page);
});

test('a double click on Create link with an attachment creates one message', async ({ page }) => {
  await openApp(page, '/');
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
  await createdLink(page);
  expect(sent).toEqual(['GET /api/v1/instance', 'POST /api/v1/messages']);
});

async function watchCreateAnswer(page: Page): Promise<() => Promise<CreateAnswer>> {
  await page.addInitScript(() => {
    let handled = false;
    let databaseOpens = 0;
    Object.defineProperty(globalThis, 'nylzoCreateAnswer', {
      value: {
        get handled() {
          return handled;
        },
        get databaseOpens() {
          return databaseOpens;
        },
      },
    });
    const open: unknown = Object.getOwnPropertyDescriptor(IDBFactory.prototype, 'open')?.value;
    Object.defineProperty(IDBFactory.prototype, 'open', {
      value(this: IDBFactory, ...parameters: Parameters<IDBFactory['open']>): IDBOpenDBRequest {
        databaseOpens += 1;
        return Reflect.apply(open as IDBFactory['open'], this, parameters);
      },
    });
    const json: unknown = Object.getOwnPropertyDescriptor(Response.prototype, 'json')?.value;
    Object.defineProperty(Response.prototype, 'json', {
      async value(this: Response): Promise<unknown> {
        const body: unknown = await Reflect.apply(json as Response['json'], this, []);
        if (this.url.endsWith('/api/v1/messages')) {
          setTimeout(() => {
            handled = true;
          }, 0);
        }
        return body;
      },
    });
  });
  return () =>
    page.evaluate(() => ({
      handled: globalThis.nylzoCreateAnswer?.handled ?? false,
      databaseOpens: globalThis.nylzoCreateAnswer?.databaseOpens ?? 0,
    }));
}

test('leaving while a message is encrypted wipes it and a later answer neither shows nor remembers it', async ({
  page,
}) => {
  const createAnswer = await watchCreateAnswer(page);
  const creation = await holdRoute(page, '**/api/v1/messages', isCreate);
  await openApp(page, '/');
  const secret = 'zebracorn marmalade quokka';
  await page.getByLabel('Message text').fill(secret);
  await page.getByRole('button', { name: 'Create link' }).click();
  await creation.requested;
  await leavePage(page);
  const shownWords = () =>
    page.evaluate((words) => words.filter((word) => document.body.textContent.includes(word)), secret.split(' '));
  expect(await shownWords()).toEqual([]);
  await expect(page.getByLabel('Message text')).toHaveValue('');

  const { databaseOpens } = await createAnswer();
  const answered = page.waitForResponse((response) => isCreate(response.request()));
  creation.release();
  const response = await answered;
  expect(response.ok()).toBe(true);
  await expect.poll(createAnswer).toEqual({ handled: true, databaseOpens });
  await expect(notInert(page, page.getByLabel('Link to the message'))).toHaveCount(0);
  expect(await shownWords()).toEqual([]);
});

async function exhaustCreatesWithoutProofOfWork(): Promise<void> {
  const api = createApi({ base: POW_BASE_URL });
  let instance = await api.instance();
  while (!instance.pow.required) {
    try {
      await createMessageThroughApi({ base: POW_BASE_URL });
    } catch (error) {
      if (!(error instanceof ApiError && error.kind === 'pow')) {
        throw error;
      }
    }
    instance = await api.instance();
  }
}

test('the client solves the proof of work the instance asks for (D19)', async ({ newPage }) => {
  await exhaustCreatesWithoutProofOfWork();
  const sender = await newPage();
  const violations = await watchViolations(sender);
  const created = sender.waitForRequest(isCreate);
  const link = await createMessageInPage(sender, { text: 'pow' }, POW_BASE_URL);
  const request = await created;
  expect(request.headers()['x-nylzo-pow']).toMatch(/^[\w-]{48}\.[\da-f]{16}$/);
  expect(await violations()).toEqual([]);

  const recipient = await newPage();
  await showMessage(recipient, link);
  await expectMessageShown(recipient);
  await expect(recipient.locator('pre')).toHaveText('pow');
});
