import { defineConfig, devices } from '@playwright/test';

const ci = Boolean(process.env['CI']);

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: ci,
  retries: 0,
  timeout: 60_000,
  reporter: ci ? 'github' : 'list',
  use: { baseURL: 'http://127.0.0.1:8090', locale: 'en-US', reducedMotion: 'reduce', trace: 'retain-on-failure' },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
  webServer: [
    {
      command: 'node serve.ts 8090',
      url: 'http://127.0.0.1:8090/healthz',
      reuseExistingServer: !ci,
      timeout: 180_000,
    },
    {
      command: 'node serve.ts 8091 pow',
      url: 'http://127.0.0.1:8091/healthz',
      reuseExistingServer: !ci,
      timeout: 180_000,
    },
  ],
});
