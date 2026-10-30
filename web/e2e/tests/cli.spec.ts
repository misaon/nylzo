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
  const bytes = Buffer.from(Array.from({ length: 256 * 1024 }, (_, index) => index % 251));
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

test(
  'a compressed CLI attachment opens and downloads unchanged in every browser',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const directory = mkdtempSync(path.join(tmpdir(), 'nylzo-compressed-'));
    const file = path.join(directory, 'rows.csv');
    const bytes = Buffer.from('row,1,alpha\n'.repeat(20_000));
    writeFileSync(file, bytes, { mode: 0o600 });
    const link = runCli(['create', '--server', BASE_URL, '--from-file', file], '').trim();
    await showMessage(page, link);
    await expectMessageShown(page);
    const downloading = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download attachment' }).click();
    const download = await downloading;
    expect(download.suggestedFilename()).toBe('rows.csv');
    const saved = await download.path();
    expect(saved).not.toBeNull();
    expect(readFileSync(saved).equals(bytes)).toBe(true);
  },
);

test(
  'the browser compresses an attachment before upload and the CLI restores its bytes',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const bytes = Buffer.from('row,1,alpha\n'.repeat(20_000));
    const upload = page.waitForRequest(
      (request) => request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/messages',
    );
    const link = await createMessageInPage(page, {
      text: 'separate uncompressed message',
      file: { name: 'rows.csv', mimeType: 'text/csv', buffer: bytes },
    });
    const request = await upload;
    expect(request.postDataBuffer()?.length).toBe(65_536);
    const directory = mkdtempSync(path.join(tmpdir(), 'nylzo-compressed-out-'));
    const lines = runCli(['read', '--out', directory], link).trim().split('\n');
    const text = lines.find((file) => path.basename(file) === 'message.txt') ?? '';
    expect(readFileSync(text, 'utf8')).toBe('separate uncompressed message');
    const saved = lines.find((file) => path.basename(file) === 'rows.csv') ?? '';
    expect(path.basename(saved)).toBe('rows.csv');
    expect(readFileSync(saved).equals(bytes)).toBe(true);
  },
);
