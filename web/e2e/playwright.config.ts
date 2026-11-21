import { defineConfig, devices } from '@playwright/test';

import { ADMIN_BASE_URL, BASE_URL, OFFICIAL_BASE_URL, POW_BASE_URL } from './tests/support/servers.ts';

const ci = Boolean(process.env['CI']);
const crossBrowser = /@cross-browser/u;

function server({ baseUrl, profile }: { readonly baseUrl: string; readonly profile: string }) {
  return {
    command: `bun serve.ts ${new URL(baseUrl).port} ${profile}`,
    url: `http://127.0.0.1:${new URL(baseUrl).port}/healthz`,
    reuseExistingServer: !ci,
    timeout: 180_000,
  };
}

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: ci,
  retries: 0,
  workers: ci ? 2 : '50%',
  timeout: 60_000,
  reporter: ci ? 'github' : 'list',
  use: { baseURL: BASE_URL, locale: 'en-US', reducedMotion: 'reduce', trace: 'retain-on-failure' },
  projects: [
    // The loopback servers stand in for public instances, which Local Network Access does not restrict.
    { name: 'chromium', use: { ...devices['Desktop Chrome'], permissions: ['local-network-access'] } },
    { name: 'firefox', use: devices['Desktop Firefox'], grep: crossBrowser },
    { name: 'webkit', use: devices['Desktop Safari'], grep: crossBrowser },
  ],
  webServer: [
    server({ baseUrl: BASE_URL, profile: 'default' }),
    server({ baseUrl: POW_BASE_URL, profile: 'pow' }),
    server({ baseUrl: OFFICIAL_BASE_URL, profile: 'official' }),
    server({ baseUrl: ADMIN_BASE_URL, profile: 'admin' }),
  ],
});
