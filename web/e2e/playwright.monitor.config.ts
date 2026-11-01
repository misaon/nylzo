import { defineConfig } from '@playwright/test';

// The installed Google Chrome, with a window under xvfb, sends what an ordinary visitor's browser sends.
export default defineConfig({
  testDir: './monitor',
  retries: 1,
  workers: 1,
  timeout: 120_000,
  reporter: 'github',
  use: { channel: 'chrome', headless: false, locale: 'en-US', trace: 'off', screenshot: 'off', video: 'off' },
});
