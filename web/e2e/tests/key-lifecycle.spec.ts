import type { Page } from '@playwright/test';

import { expect, test } from './support/fixtures.ts';
import { sentRecords } from './support/indexed-database.ts';
import { createMessageThroughApi } from './support/messages.ts';
import { createMessageInPage, expectMessageShown, showMessage } from './support/pages.ts';

interface KeyAudit {
  privateExports: number;
  extractablePrivateKeys: number;
  nativeSignatures: number;
  jwkImports: number;
}

declare global {
  var nylzoKeyAudit: KeyAudit;
}

async function auditKeys(page: Page, refusePkcs8: boolean): Promise<void> {
  await page.addInitScript((refuse) => {
    const audit = { privateExports: 0, extractablePrivateKeys: 0, nativeSignatures: 0, jwkImports: 0 };
    Object.defineProperty(globalThis, 'nylzoKeyAudit', { value: audit });
    const prototype = SubtleCrypto.prototype;
    const importKey = crypto.subtle.importKey.bind(crypto.subtle);
    const exportKey = crypto.subtle.exportKey.bind(crypto.subtle);
    const sign = crypto.subtle.sign.bind(crypto.subtle);
    Object.defineProperties(prototype, {
      importKey: {
        value(this: SubtleCrypto, ...parameters: Parameters<typeof importKey>) {
          const format = parameters[0] as KeyFormat;
          if (format === 'pkcs8' && refuse) {
            return Promise.reject(new DOMException('simulated PKCS#8 rejection', 'DataError'));
          }
          const extractable = parameters[3];
          if (extractable && (format === 'pkcs8' || format === 'jwk')) {
            audit.extractablePrivateKeys++;
          }
          if (format === 'jwk') {
            audit.jwkImports++;
          }
          return Reflect.apply(importKey, this, parameters);
        },
      },
      exportKey: {
        value(this: SubtleCrypto, ...parameters: Parameters<typeof exportKey>) {
          if (parameters[1].type === 'private') {
            audit.privateExports++;
          }
          return Reflect.apply(exportKey, this, parameters);
        },
      },
      sign: {
        value(this: SubtleCrypto, ...parameters: Parameters<typeof sign>) {
          audit.nativeSignatures++;
          return Reflect.apply(sign, this, parameters);
        },
      },
    });
  }, refusePkcs8);
}

async function resumeRecords(page: Page) {
  return page.evaluate(async () => {
    const database = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('nylzo-resume');
      request.addEventListener('success', () => resolve(request.result));
      request.addEventListener('error', () => reject(request.error ?? new Error('the resume store did not open')));
    });
    try {
      const values = await new Promise<unknown[]>((resolve, reject) => {
        const request = database.transaction('keys').objectStore('keys').getAll();
        request.addEventListener('success', () => resolve(request.result));
        request.addEventListener('error', () => reject(request.error ?? new Error('the resume store did not open')));
      });
      const records = values as { key: CryptoKey; ciphertext: ArrayBuffer; token: string }[];
      return await Promise.all(
        records.map(async (record) => {
          const wrappingKey = await crypto.subtle.generateKey({ name: 'AES-KW', length: 256 }, false, ['wrapKey']);
          const exports = await Promise.allSettled([
            crypto.subtle.exportKey('raw', record.key),
            crypto.subtle.exportKey('jwk', record.key),
            crypto.subtle.wrapKey('raw', record.key, wrappingKey, 'AES-KW'),
          ]);
          return {
            fields: Object.keys(record).toSorted((left, right) => left.localeCompare(right)),
            token: record.token,
            extractable: record.key.extractable,
            algorithm: record.key.algorithm,
            usages: record.key.usages.toSorted((left, right) => left.localeCompare(right)),
            ciphertextLength: record.ciphertext.byteLength,
            exports: exports.map((result) => result.status),
          };
        }),
      );
    } finally {
      database.close();
    }
  });
}

type Mutation = 'ciphertext' | 'expired' | 'expiry' | 'extractable' | 'message';

async function corruptRecovery(page: Page, mutation: Mutation): Promise<void> {
  await page.evaluate(async (change) => {
    const database = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('nylzo-resume');
      request.addEventListener('success', () => resolve(request.result));
      request.addEventListener('error', () => reject(request.error ?? new Error('store unavailable')));
    });
    try {
      const values = await new Promise<unknown[]>((resolve, reject) => {
        const request = database.transaction('keys').objectStore('keys').getAll();
        request.addEventListener('success', () => resolve(request.result));
        request.addEventListener('error', () => reject(request.error ?? new Error('record unavailable')));
      });
      const record = values[0] as { id: string; expiresAt: number; key: CryptoKey; ciphertext: ArrayBuffer };
      switch (change) {
        case 'ciphertext': {
          const bytes = new Uint8Array(record.ciphertext);
          bytes[0] = (bytes[0] ?? 0) ^ 1;
          break;
        }
        case 'expired': {
          record.expiresAt = Date.now() - 1;
          break;
        }
        case 'expiry': {
          record.expiresAt -= 1;
          break;
        }
        case 'extractable': {
          record.key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, true, ['encrypt', 'decrypt']);
          break;
        }
        case 'message': {
          record.id = 'AAAAAAAAAAAAAAAAAAAAAA';
          break;
        }
      }
      await new Promise<void>((resolve, reject) => {
        const transaction = database.transaction('keys', 'readwrite');
        transaction.objectStore('keys').put(record);
        transaction.addEventListener('complete', () => resolve());
        transaction.addEventListener('abort', () => reject(transaction.error ?? new Error('write failed')));
      });
    } finally {
      database.close();
    }
  }, mutation);
}

for (const refusePkcs8 of [false, true]) {
  test(
    `private keys stay native and non-extractable (PKCS#8 refusal: ${refusePkcs8})`,
    { tag: '@cross-browser' },
    async ({ newPage }) => {
      const sender = await newPage();
      await auditKeys(sender, refusePkcs8);
      const link = await createMessageInPage(sender, { text: 'native keys', remember: true });
      await expect(sender.getByText('This device remembered the message.')).toBeVisible();
      const senderAudit = await sender.evaluate(() => globalThis.nylzoKeyAudit);
      expect(senderAudit.privateExports).toBe(0);
      expect(senderAudit.extractablePrivateKeys).toBe(0);
      const stored = await sentRecords(sender);
      expect(stored).toHaveLength(1);
      expect(stored[0]?.privateKey).toEqual({
        algorithm: 'Ed25519',
        extractable: false,
        usages: ['sign'],
        exportable: false,
      });
      const recipient = await newPage();
      await auditKeys(recipient, refusePkcs8);
      await showMessage(recipient, link);
      await expectMessageShown(recipient);
      await expect(recipient.locator('pre')).toHaveText('native keys');
      const recipientAudit = await recipient.evaluate(() => globalThis.nylzoKeyAudit);
      expect(recipientAudit.privateExports).toBe(0);
      expect(recipientAudit.extractablePrivateKeys).toBe(0);
      expect(recipientAudit.nativeSignatures).toBeGreaterThan(0);
      expect(recipientAudit.jwkImports).toBeGreaterThanOrEqual(Number(refusePkcs8));
    },
  );
}

test(
  'an interrupted open stores only an opaque session pointer and an encrypted fragment with a non-extractable IndexedDB key, then removes both after recovery',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi({
      text: 'resume without plaintext keys',
      readKeyStartsWithZero: true,
    });
    await page.route('**/api/v1/messages/*/claim', async (route) => {
      const response = await route.fetch();
      await route.fulfill({ response, body: Buffer.alloc(0) });
    });
    await showMessage(page, message.link);
    await expect(page.getByText('The message cannot be opened safely')).toBeVisible();
    const session = await page.evaluate(() =>
      Object.fromEntries(Object.keys(sessionStorage).map((key) => [key, sessionStorage.getItem(key)])),
    );
    const pointer = session[`nylzo:link:${message.id}`];
    expect(pointer).toMatch(/^[\w-]{43}$/);
    expect(JSON.stringify(session)).not.toContain(message.secret);
    const records = await resumeRecords(page);
    expect(records).toHaveLength(1);
    expect(records[0]).toMatchObject({
      token: pointer,
      fields: ['ciphertext', 'expiresAt', 'id', 'iv', 'key', 'token'],
      extractable: false,
      algorithm: { name: 'AES-GCM', length: 256 },
      usages: ['decrypt', 'encrypt'],
      exports: ['rejected', 'rejected', 'rejected'],
    });
    expect(records[0]?.ciphertextLength).toBeGreaterThan(new URL(message.link).hash.length);
    await page.unrouteAll();
    await page.reload();
    await expect(page.getByText('You already opened the message on this page')).toBeVisible();
    const acknowledgement = page.waitForResponse((response) => response.url().endsWith('/ack'));
    await page.getByRole('button', { name: 'Show message' }).click();
    await expectMessageShown(page);
    await expect(page.locator('pre')).toHaveText('resume without plaintext keys');
    const acknowledged = await acknowledgement;
    expect(acknowledged.status()).toBe(204);
    await expect.poll(() => resumeRecords(page)).toEqual([]);
    expect(await page.evaluate(() => sessionStorage.getItem(`nylzo:link:${location.pathname.slice(3)}`))).toBeNull();
  },
);

for (const mutation of ['ciphertext', 'expired', 'expiry', 'extractable', 'message'] as const) {
  test(
    `rejects damaged recovery data (${mutation}) without sending a read request`,
    { tag: '@cross-browser' },
    async ({ page }) => {
      const message = await createMessageThroughApi({ text: 'never reveal corrupted recovery' });
      await page.route('**/api/v1/messages/*/claim', async (route) => {
        const response = await route.fetch();
        await route.fulfill({ response, body: Buffer.alloc(0) });
      });
      await showMessage(page, message.link);
      await expect(page.getByText('The message cannot be opened safely')).toBeVisible();
      await corruptRecovery(page, mutation);
      const reads: string[] = [];
      page.on('request', (request) => {
        if (request.url().includes('/api/v1/messages/')) {
          reads.push(request.url());
        }
      });
      await page.reload();
      await expect(page.getByText('The key is no longer in the address bar', { exact: true })).toBeVisible();
      expect(reads).toEqual([]);
      await expect.poll(() => resumeRecords(page)).toEqual([]);
    },
  );
}
