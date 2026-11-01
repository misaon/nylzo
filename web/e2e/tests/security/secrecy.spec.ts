import type { Locator } from '@playwright/test';

import { deriveContent, deriveLink, encodeBase64url, parseLink, requestKey } from '@nylzo/core';
import { scryptPassphraseKey } from '@nylzo/core/scrypt';

import { expect, test } from '../support/fixtures.ts';
import { createMessageThroughApi } from '../support/messages.ts';
import { createMessageInPage, expectMessageShown, notInert, openApp, showMessage } from '../support/pages.ts';
import { encodedForms, expectNoSecretSent, recordRequests, textForms } from '../support/requests.ts';
import { watchViolations } from '../support/watchers.ts';

const PASSPHRASE = 'four random words here';
const WRONG_PASSPHRASE = 'definitely the wrong one';

test(
  'a message with a passphrase and a file goes between two browsers under the enforced CSP, a wrong passphrase spends no open and no secret leaves either browser (AK1, AK2, AK5, AK10)',
  { tag: '@cross-browser' },
  async ({ newPage }) => {
    const sender = await newPage();
    const recipient = await newPage();
    const senderRequests = recordRequests(sender);
    const recipientRequests = recordRequests(recipient);
    const senderViolations = await watchViolations(sender);
    const recipientViolations = await watchViolations(recipient);
    const text = 'tajné sdělení jen pro příjemce';
    const file = {
      name: 'výplatní páska říjen.pdf',
      mimeType: 'application/pdf',
      buffer: Buffer.from('%PDF-1.4\n%nylzo příloha jen pro příjemce\n'),
    };
    const link = await createMessageInPage(sender, { text, file, passphrase: PASSPHRASE });
    expect(link).toMatch(/^http:\/\/127\.0\.0\.1:8090\/m\/[\w-]{22}#1\.1\.[\w-]{43}$/);

    await showMessage(recipient, link, WRONG_PASSPHRASE);
    await expect(recipient.getByText(/The passphrase does not match/)).toBeVisible({ timeout: 30_000 });
    await recipient.getByLabel('Passphrase', { exact: true }).fill(PASSPHRASE);
    await recipient.getByRole('button', { name: 'Show message' }).click();
    await expectMessageShown(recipient);
    await expect(recipient.locator('pre')).toHaveText(text);
    await expect(recipient.locator('bdi')).toHaveText(file.name);
    expect(new URL(recipient.url()).hash).toBe('');
    await expect(
      notInert(recipient, recipient.getByRole('button', { name: 'Delete the remaining opens' })),
    ).toHaveCount(0);
    await expect(notInert(recipient, recipient.getByRole('button', { name: 'Report', exact: true }))).toHaveCount(0);
    expect(await senderViolations()).toEqual([]);
    expect(await recipientViolations()).toEqual([]);

    const late = await newPage();
    await late.goto(link);
    await expect(late.getByText('The message has already been opened')).toBeVisible();

    const { id, secret } = parseLink(link);
    const { readSeed, passphraseSalt } = await deriveLink(id, secret);
    const passphraseKey = scryptPassphraseKey(PASSPHRASE, passphraseSalt);
    const { verifierSeed } = await deriveContent(id, secret, passphraseKey);
    const readKey = await requestKey(readSeed);
    const verifierKey = await requestKey(verifierSeed);
    const requests = [...(await senderRequests()), ...(await recipientRequests())];
    const creation = requests.find((request) => request.headers['x-nylzo-create'] !== undefined);
    const announced = Buffer.from(creation?.headers['x-nylzo-create'] ?? '', 'base64url').toString('utf8');
    expect(JSON.parse(announced)).toMatchObject({
      read_key: encodeBase64url(readKey.publicKey),
      verifier_key: encodeBase64url(verifierKey.publicKey),
    });
    expect(requests.length).toBeGreaterThan(6);
    expectNoSecretSent(requests, [
      new URL(link).hash.slice(1),
      ...textForms(PASSPHRASE),
      ...textForms(WRONG_PASSPHRASE),
      ...textForms(text),
      ...textForms(file.name),
      ...textForms(file.buffer.toString('utf8')),
      ...[secret, readSeed, passphraseSalt, passphraseKey, verifierSeed].flatMap((value) => encodedForms(value)),
    ]);
  },
);

test(
  'no field for a secret offers it to spell checkers, translators, writing assistants or password managers (D38, D41, W2-03, W2-05)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi({ passphrase: PASSPHRASE, opensMax: 2 });
    const quiet = {
      spellcheck: 'false',
      autocorrect: 'off',
      autocapitalize: 'off',
      autocomplete: 'off',
      translate: 'no',
      writingsuggestions: 'false',
    };
    const passphrase = {
      ...quiet,
      autocomplete: 'one-time-code',
      'data-1p-ignore': '',
      'data-lpignore': 'true',
      'data-bwignore': 'true',
      'data-protonpass-ignore': 'true',
    };
    const passwordFields = page.locator('[autocomplete*="password"], [autocomplete="username"]');

    await openApp(page, '/');
    await expectAttributes(page.getByLabel('Message text'), quiet);
    await expectAttributes(page.getByLabel('Passphrase, optional'), passphrase);
    await expectAttributes(page.getByLabel('Link to open'), quiet);
    expect(await formOwner(page.getByLabel('Passphrase, optional'))).toBeNull();
    await expect(passwordFields).toHaveCount(0);

    await page.goto(message.link);
    const field = page.getByLabel('Passphrase', { exact: true });
    await expectAttributes(field, passphrase);
    expect(await formOwner(field)).toBeNull();
    await page.getByRole('button', { name: 'Report', exact: true }).click();
    await expectAttributes(page.getByLabel('Reason, optional'), quiet);
    await expect(passwordFields).toHaveCount(0);
    await page.getByRole('button', { name: 'Report', exact: true }).click();

    const filled: string[] = [];
    await page.route('**/api/v1/messages/*/claim', async (route) => {
      filled.push(
        ...(await page.evaluate(() =>
          [...document.querySelectorAll('input')]
            .filter((input) => ['password', 'text'].includes(input.type) && input.value !== '')
            .map((input) => input.type),
        )),
      );
      await route.continue();
    });
    await field.fill(PASSPHRASE);
    await field.press('Enter');
    await expectMessageShown(page);
    expect(filled).toEqual([]);
    await expect(page.locator('pre')).toHaveAttribute('translate', 'no');

    await page.goto('/report');
    await expectAttributes(page.getByLabel('Link to the message'), quiet);
    await expectAttributes(page.getByLabel('Reason, optional'), quiet);
  },
);

async function formOwner(field: Locator): Promise<string | null> {
  return field.evaluate((element) =>
    element instanceof HTMLInputElement ? (element.form?.id ?? null) : 'not an input field',
  );
}

async function expectAttributes(field: Locator, attributes: Record<string, string>): Promise<void> {
  for (const [name, value] of Object.entries(attributes)) {
    await expect(field, name).toHaveAttribute(name, value);
  }
}
