import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { BASE_URL, NYLZO_BIN, createViaUi, expect, expectRevealed, openViaUi, test } from './helpers.ts';

const PASSPHRASE = 'correct horse battery staple';

function cli(args: readonly string[], input: string, env: Record<string, string> = {}): string {
  return execFileSync(NYLZO_BIN, args, { input, encoding: 'utf8', env: { ...process.env, ...env }, stdio: ['pipe', 'pipe', 'pipe'] });
}

function passphraseFile(): string {
  const path = join(mkdtempSync(join(tmpdir(), 'nylzo-cli-')), 'passphrase');
  writeFileSync(path, PASSPHRASE, { mode: 0o600 });
  return path;
}

test('a message from the CLI opens in the browser', async ({ page }) => {
  const link = cli(['create', '--server', BASE_URL], 'z příkazové řádky').trim();
  await openViaUi(page, link);
  await expectRevealed(page);
  await expect(page.locator('pre')).toHaveText('z příkazové řádky');
});

test('a message with a passphrase from the CLI opens in the browser', async ({ page }) => {
  for (const kdf of ['scrypt', 'pbkdf2']) {
    const link = cli(['create', '--server', BASE_URL, '--passphrase-file', passphraseFile(), '--kdf', kdf], `chráněno ${kdf}`).trim();
    await openViaUi(page, link, PASSPHRASE);
    await expectRevealed(page);
    await expect(page.locator('pre')).toHaveText(`chráněno ${kdf}`);
  }
});

test('the CLI reads messages and files created in the browser', async ({ page }) => {
  const plain = await createViaUi(page, { text: 'z prohlížeče' });
  expect(cli(['read', '--stdout'], plain)).toBe('z prohlížeče\n');
  const guarded = await createViaUi(page, { text: 's heslem', passphrase: PASSPHRASE, kdf: 'scrypt' });
  expect(cli(['read', '--stdout', '--passphrase-file', passphraseFile()], guarded)).toBe('s heslem\n');
  const bytes = Buffer.from(Array.from({ length: 70_000 }, (_, i) => i % 251));
  const withFile = await createViaUi(page, { text: '', file: { name: 'data.bin', mimeType: 'application/octet-stream', buffer: bytes } });
  const out = mkdtempSync(join(tmpdir(), 'nylzo-out-'));
  const paths = cli(['read', '--out', out], withFile).trim().split('\n');
  expect(paths).toEqual([join(out, 'data.bin')]);
  expect(readFileSync(paths[0] ?? '').equals(bytes)).toBe(true);
});
