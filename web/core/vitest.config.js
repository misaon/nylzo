import { playwright } from '@vitest/browser-playwright';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    projects: [
      { test: { name: 'bun', include: ['src/**/*.test.ts'], exclude: ['src/envelope/compression.test.ts'] } },
      {
        test: {
          name: 'browser-compression',
          include: ['src/envelope/compression.test.ts'],
          browser: { enabled: true, headless: true, provider: playwright(), instances: [{ browser: 'chromium' }] },
        },
      },
    ],
    coverage: {
      provider: 'istanbul',
      reporter: ['text', 'lcov', 'html'],
      include: ['src/**/*.ts'],
      exclude: ['src/**/*.test.ts', 'src/**/*.d.ts'],
    },
  },
});
