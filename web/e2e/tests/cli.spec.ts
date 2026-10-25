import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

import { expect, test } from './support/fixtures.ts';
import { createMessageInPage, expectMessageShown, showMessage } from './support/pages.ts';
import { BASE_URL, NYLZO_BINARY } from './support/servers.ts';

const PASSPHRASE = 'correct horse battery staple';

function runCli(parameters: readonly string[], input: string): string {
  return execFileSync(NYLZO_BINARY, parameters, { input, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] });
}

function passphraseFile(): string {
  const directory = mkdtempSync(path.join(tmpdir(), 'nylzo-cli-'));
  const file = path.join(directory, 'passphrase');
  writeFileSync(file, PASSPHRASE, { mode: 0o600 });
  return file;
}

test('messages from the CLI, with and without a passphrase, open in the browser', async ({ page }) => {
  const plain = runCli(['create', '--server', BASE_URL], 'z příkazové řádky').trim();
  expect(plain).toContain('#1.0.');
  await showMessage(page, plain);
  await expectMessageShown(page);
  await expect(page.locator('pre')).toHaveText('z příkazové řádky');

  const guarded = runCli(['create', '--server', BASE_URL, '--passphrase-file', passphraseFile()], 'chráněno').trim();
  expect(guarded).toContain('#1.1.');
  await showMessage(page, guarded, PASSPHRASE);
  await expectMessageShown(page);
  await expect(page.locator('pre')).toHaveText('chráněno');
});

test('the CLI reads messages and files created in the browser', async ({ page }) => {
  const plain = await createMessageInPage(page, { text: 'z prohlížeče' });
  expect(runCli(['read', '--stdout'], plain)).toBe('z prohlížeče\n');
  const guarded = await createMessageInPage(page, { text: 's heslem', passphrase: PASSPHRASE });
  expect(runCli(['read', '--stdout', '--passphrase-file', passphraseFile()], guarded)).toBe('s heslem\n');
  const bytes = Buffer.from(Array.from({ length: 70_000 }, (_, index) => index % 251));
  const withFile = await createMessageInPage(page, {
    file: { name: 'data.bin', mimeType: 'application/octet-stream', buffer: bytes },
  });
  const output = mkdtempSync(path.join(tmpdir(), 'nylzo-out-'));
  const written = runCli(['read', '--out', output], withFile).trim().split('\n');
  expect(written).toHaveLength(1);
  const [file = ''] = written;
  expect(path.dirname(path.dirname(file))).toBe(output);
  expect(path.basename(file)).toBe('data.bin');
  expect(readFileSync(file).equals(bytes)).toBe(true);
});
