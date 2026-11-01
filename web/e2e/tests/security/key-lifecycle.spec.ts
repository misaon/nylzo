import type { Page } from '@playwright/test';

import { expect, test } from '../support/fixtures.ts';
import { sentRecords } from '../support/indexed-database.ts';
import { createMessageInPage, expectMessageShown, showMessage } from '../support/pages.ts';

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
