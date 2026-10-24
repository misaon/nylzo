import {
  KDF_NONE,
  deriveContent,
  deriveLink,
  encodeBase64url,
  formatLink,
  idToString,
  newHeader,
  newSecret,
  requestKey,
  seal,
  type KdfId,
  type RequestKey,
} from '@nylzo/core';
import { anyPassphraseKey } from '@nylzo/core/scrypt';
import { test as base, expect, type Locator, type Page, type Response } from '@playwright/test';

import { createApi, type Api } from '../../app/src/lib/api.ts';

export { expect };

export const test = base.extend<{ closeOwnContexts: void }>({
  closeOwnContexts: [
    async ({ browser }, use) => {
      const before = new Set(browser.contexts());
      await use();
      await Promise.all(browser.contexts().filter((context) => !before.has(context)).map((context) => context.close()));
    },
    { auto: true },
  ],
});

export const BASE_URL = 'http://127.0.0.1:8090';
export const POW_BASE_URL = 'http://127.0.0.1:8091';
export const NYLZO_BIN = new URL('../.bin/8090/nylzo', import.meta.url).pathname;

export interface ApiMessage {
  readonly link: string;
  readonly id: Uint8Array;
  readonly idText: string;
  readonly secret: string;
  readonly manageKey: RequestKey;
  readonly api: Api;
}

export interface ApiMessageOptions {
  readonly base?: string;
  readonly text?: string;
  readonly passphrase?: string;
  readonly kdfId?: KdfId;
  readonly opensMax?: number;
  readonly readKeyStartsWithZero?: boolean;
}

export async function createViaApi(options: ApiMessageOptions = {}): Promise<ApiMessage> {
  const base = options.base ?? BASE_URL;
  const kdfId = options.kdfId ?? KDF_NONE;
  const opensMax = options.opensMax ?? 1;
  const api = createApi({ base });
  const { create } = await api.instance();
  let s = newSecret();
  let link = await deriveLink(create.id, s);
  while (options.readKeyStartsWithZero && link.r[0] !== 0) {
    s = newSecret();
    link = await deriveLink(create.id, s);
  }
  const p = await anyPassphraseKey(kdfId, options.passphrase ?? '', link.saltPw);
  const content = await deriveContent(create.id, s, p);
  const policy = { opensMax, ttlSeconds: 3600 };
  const object = await seal(content.k, newHeader(kdfId), create.id, policy, { meta: { text: options.text ?? 'from the api' } });
  const readKey = await requestKey(link.r);
  const manageKey = await requestKey(newSecret());
  const verifierKey = kdfId === KDF_NONE ? undefined : (await requestKey(content.v)).publicKey;
  await api.create({
    id: create.id,
    idToken: create.idToken,
    readKey: readKey.publicKey,
    ...(verifierKey ? { verifierKey } : {}),
    manageKey: manageKey.publicKey,
    kdfId,
    ...policy,
    object,
  });
  return {
    link: formatLink(base, create.id, kdfId, s),
    id: create.id,
    idText: idToString(create.id),
    secret: encodeBase64url(s),
    manageKey,
    api,
  };
}

export async function openApp(page: Page, url: string): Promise<Response | null> {
  const response = await page.goto(url);
  await page.locator('html[data-ready]').waitFor({ state: 'attached' });
  return response;
}

export interface UiMessage {
  readonly text?: string;
  readonly file?: { readonly name: string; readonly mimeType: string; readonly buffer: Buffer };
  readonly passphrase?: string;
}

export async function createViaUi(page: Page, message: UiMessage, base = BASE_URL): Promise<string> {
  await openApp(page, `${base}/`);
  await page.getByLabel('Message text').fill(message.text ?? '');
  if (message.file) {
    await page.getByLabel(/Attachment, up to/).setInputFiles(message.file);
  }
  if (message.passphrase) {
    await setPassphrase(page, message.passphrase);
  }
  await page.getByRole('button', { name: 'Create link' }).click();
  const link = page.getByLabel('Link to the message');
  await expect(link).toBeVisible({ timeout: 45_000 });
  return link.inputValue();
}

export async function setPassphrase(page: Page, passphrase: string): Promise<void> {
  await page.getByRole('button', { name: 'Passphrase', exact: true }).click();
  const protect = page.getByRole('switch', { name: /^Protect with a passphrase/ });
  if ((await protect.getAttribute('aria-checked')) !== 'true') {
    await protect.click();
  }
  await page.getByLabel('Passphrase, optional').fill(passphrase);
}

export async function leave(page: Page): Promise<void> {
  await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true })));
}

export function live(page: Page, locator: Locator): Locator {
  return locator.and(page.locator(':not([inert]):not([inert] *)'));
}

export async function openViaUi(page: Page, link: string, passphrase?: string): Promise<void> {
  await page.goto(link);
  if (passphrase !== undefined) {
    await page.getByLabel('Passphrase', { exact: true }).fill(passphrase);
  }
  await page.getByRole('button', { name: 'Show message' }).click();
}

export async function expectRevealed(page: Page): Promise<void> {
  await expect(page.getByRole('heading', { name: 'Message content' })).toBeVisible({ timeout: 45_000 });
}
