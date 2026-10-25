import { defineConfig, devices } from '@playwright/test';

import { BASE_URL, POW_BASE_URL } from './tests/support/servers.ts';

const ci = Boolean(process.env['CI']);

function server(baseUrl: string, profile: string) {
  return {
    command: `node serve.ts ${new URL(baseUrl).port} ${profile}`,
    url: `${baseUrl}/healthz`,
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
    { name: 'chromium', use: devices['Desktop Chrome'] },
    { name: 'firefox', use: devices['Desktop Firefox'] },
    { name: 'webkit', use: devices['Desktop Safari'] },
  ],
  webServer: [server(BASE_URL, 'default'), server(POW_BASE_URL, 'pow')],
});
