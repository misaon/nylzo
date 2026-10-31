import { execFileSync } from 'node:child_process';

import { expect, test } from '../support/fixtures.ts';
import { BASE_URL, NYLZO_BINARY } from '../support/servers.ts';

interface ClientManifest {
  readonly server: string;
  readonly build: ClientBuild;
  readonly files: readonly { readonly path: string; readonly sha256: string }[];
}

interface ClientBuild {
  readonly version: string;
  readonly commit: string;
  readonly date: string;
  readonly dirty: boolean;
}

test('nylzo verify-client finds the embedded web client unchanged on the server (D61)', () => {
  const output = execFileSync(NYLZO_BINARY, ['verify-client', '--server', BASE_URL], {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  expect(output).toMatch(/^the server sends the web client of nylzo \S+ unchanged: 6 pages and \d+ files checked\n$/);
});

test('the transparency page shows its embedded client build, reported server version and file hashes (D61)', async ({
  page,
  request,
}) => {
  const response = await request.get('/.well-known/nylzo/client.json');
  const manifest = (await response.json()) as ClientManifest;
  const buildResponse = await request.get('/assets/build-info.json');
  const build = (await buildResponse.json()) as ClientBuild;
  expect(manifest.files.length).toBeGreaterThan(1);
  const binary = JSON.parse(execFileSync(NYLZO_BINARY, ['version', '--json'], { encoding: 'utf8' })) as ClientBuild;
  const instanceResponse = await request.get('/api/v1/instance');
  const instance = (await instanceResponse.json()) as { build: ClientBuild };
  const transparencyResponse = await request.get('/.well-known/nylzo/transparency.json');
  const transparency = (await transparencyResponse.json()) as { build: ClientBuild };
  expect(binary).toEqual(build);
  expect(manifest.build).toEqual(build);
  expect(instance.build).toEqual(build);
  expect(transparency.build).toEqual(build);
  await page.goto('/transparency');
  await expect(page.getByTestId('client-version')).toHaveText(build.version);
  await expect(page.getByTestId('client-commit')).toHaveText(build.commit);
  await expect(page.getByTestId('server-version')).toHaveText(manifest.server);
  await expect(page.locator('footer').getByText(build.version, { exact: true })).toBeVisible();
  for (const file of manifest.files) {
    await expect(page.getByText(file.sha256, { exact: true })).toBeVisible();
  }
  await expect(page.getByText(`nylzo verify-client --server ${BASE_URL}`)).toBeVisible();
});

test('the client version stays independent of server reports', async ({ page, request }) => {
  const response = await request.get('/assets/build-info.json');
  const build = (await response.json()) as ClientBuild;
  const reportedVersion = '99.98.97-server';
  await page.route('**/.well-known/nylzo/*.json', async (route) => {
    const response = await route.fetch();
    const report = (await response.json()) as Record<string, unknown>;
    await route.fulfill({ response, json: { ...report, server: reportedVersion } });
  });
  await page.goto('/transparency');
  await expect(page.getByTestId('client-version')).toHaveText(build.version);
  await expect(page.getByTestId('client-commit')).toHaveText(build.commit);
  await expect(page.getByTestId('server-version')).toHaveText(reportedVersion);
});
