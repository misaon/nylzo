import type { Page } from '@playwright/test';

declare global {
  interface Window {
    nylzoViolations: string[];
  }
}

export async function recordViolations(page: Page): Promise<() => Promise<string[]>> {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(`pageerror: ${error.message}`));
  await page.addInitScript(() => {
    window.nylzoViolations = [];
    document.addEventListener('securitypolicyviolation', (event) => {
      window.nylzoViolations.push(`${event.effectiveDirective} ${event.blockedURI}`);
    });
  });
  return async () => [...errors, ...(await page.evaluate(() => window.nylzoViolations))];
}
