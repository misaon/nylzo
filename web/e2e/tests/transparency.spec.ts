import { execFileSync } from 'node:child_process';

import { expect, test } from './support/fixtures.ts';
import { BASE_URL, NYLZO_BINARY } from './support/servers.ts';

interface ClientManifest {
  readonly server: string;
  readonly files: readonly { readonly path: string; readonly sha256: string }[];
}

test('nylzo verify-client finds the embedded web client unchanged on the server (D61)', () => {
  const output = execFileSync(NYLZO_BINARY, ['verify-client', '--server', BASE_URL], {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  expect(output).toMatch(/^the server sends the web client of nylzo \S+ unchanged: 6 pages and \d+ files checked\n$/);
});

test('the transparency page shows the server version and the hash of every file the server sends (D61)', async ({
  page,
  request,
}) => {
  const response = await request.get('/.well-known/nylzo/client.json');
  const manifest = (await response.json()) as ClientManifest;
  expect(manifest.files.length).toBeGreaterThan(1);
  await page.goto('/transparency');
  await expect(page.getByText(manifest.server, { exact: true })).toBeVisible();
  for (const file of manifest.files) {
    await expect(page.getByText(file.sha256, { exact: true })).toBeVisible();
  }
  await expect(page.getByText(`nylzo verify-client --server ${BASE_URL}`)).toBeVisible();
});
