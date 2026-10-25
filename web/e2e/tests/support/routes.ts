import type { Page, Request } from '@playwright/test';

export interface HeldRoute {
  readonly requested: Promise<boolean>;
  readonly release: () => void;
}

export async function holdRoute(
  page: Page,
  url: string | RegExp,
  matches: (request: Request) => boolean = () => true,
): Promise<HeldRoute> {
  const requested = Promise.withResolvers<boolean>();
  const released = Promise.withResolvers<boolean>();
  await page.route(url, async (route) => {
    if (matches(route.request())) {
      requested.resolve(true);
      await released.promise;
    }
    await route.continue();
  });
  return {
    requested: requested.promise,
    release: () => {
      released.resolve(true);
    },
  };
}
