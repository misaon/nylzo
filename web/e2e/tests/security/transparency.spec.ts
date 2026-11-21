import { execFileSync } from 'node:child_process';

import type { BuildInfo } from '../../../../tools/build-info.mts';
import { expect, test } from '../support/fixtures.ts';
import { BASE_URL, NYLZO_BINARY } from '../support/servers.ts';

interface ClientManifest {
  readonly server: string;
  readonly build: BuildInfo;
  readonly files: readonly ClientFile[];
}

interface ClientFile {
  readonly path: string;
  readonly sha256: string;
}

function field(value: unknown, name: string): unknown {
  return typeof value === 'object' && value !== null ? Reflect.get(value, name) : undefined;
}

const BUILD_FIELDS = {
  version: 'string',
  commit: 'string',
  date: 'string',
  dirty: 'boolean',
} as const satisfies Record<keyof BuildInfo, 'string' | 'boolean'>;

function isBuildInfo(value: unknown): value is BuildInfo {
  return Object.entries(BUILD_FIELDS).every(([name, type]) => typeof field(value, name) === type);
}

function isClientFile(value: unknown): value is ClientFile {
  return typeof field(value, 'path') === 'string' && typeof field(value, 'sha256') === 'string';
}

function isClientManifest(value: unknown): value is ClientManifest {
  const files = field(value, 'files');
  return (
    typeof field(value, 'server') === 'string' &&
    isBuildInfo(field(value, 'build')) &&
    Array.isArray(files) &&
    files.every((file: unknown) => isClientFile(file))
  );
}

function buildInfoOf(value: unknown): BuildInfo {
  if (!isBuildInfo(value)) {
    throw new TypeError('the build information lacks a version, a commit, a date or the dirty flag');
  }
  return value;
}

function clientManifestOf(value: unknown): ClientManifest {
  if (!isClientManifest(value)) {
    throw new TypeError('the client manifest lacks the server version, the build or the file hashes');
  }
  return value;
}

test('nylzo verify-client finds the embedded web client unchanged on the server', () => {
  const output = execFileSync(NYLZO_BINARY, ['verify-client', '--server', BASE_URL], {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  expect(output).toMatch(/^the server sends the web client of nylzo \S+ unchanged: 18 pages and \d+ files checked\n$/u);
});

test('the transparency page shows its embedded client build, reported server version and file hashes', async ({
  page,
  request,
}) => {
  const response = await request.get('/.well-known/nylzo/client.json');
  const manifest = clientManifestOf(await response.json());
  const buildResponse = await request.get('/assets/build-info.json');
  const build = buildInfoOf(await buildResponse.json());
  expect(manifest.files.length).toBeGreaterThan(1);
  const binary: unknown = JSON.parse(execFileSync(NYLZO_BINARY, ['version', '--json'], { encoding: 'utf8' }));
  const instanceResponse = await request.get('/api/v1/instance');
  const transparencyResponse = await request.get('/.well-known/nylzo/transparency.json');
  expect(binary).toEqual(build);
  expect(manifest.build).toEqual(build);
  expect(field(await instanceResponse.json(), 'build')).toEqual(build);
  expect(field(await transparencyResponse.json(), 'build')).toEqual(build);
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
  const build = buildInfoOf(await response.json());
  const reportedVersion = '99.98.97-server';
  await page.route('**/.well-known/nylzo/*.json', async (route) => {
    const upstream = await route.fetch();
    const report: unknown = await upstream.json();
    const fields = typeof report === 'object' ? report : null;
    await route.fulfill({ response: upstream, json: { ...fields, server: reportedVersion } });
  });
  await page.goto('/transparency');
  await expect(page.getByTestId('client-version')).toHaveText(build.version);
  await expect(page.getByTestId('client-commit')).toHaveText(build.commit);
  await expect(page.getByTestId('server-version')).toHaveText(reportedVersion);
});
