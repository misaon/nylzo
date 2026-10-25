import { test as base, type BrowserContext, type BrowserContextOptions, type Page } from '@playwright/test';

export { expect } from '@playwright/test';

interface Fixtures {
  readonly newPage: (options?: BrowserContextOptions) => Promise<Page>;
}

export const test = base.extend<Fixtures>({
  newPage: async ({ browser }, use) => {
    const contexts: BrowserContext[] = [];
    await use(async (options = {}) => {
      const context = await browser.newContext(options);
      contexts.push(context);
      return context.newPage();
    });
    await Promise.all(contexts.map((context) => context.close()));
  },
});
