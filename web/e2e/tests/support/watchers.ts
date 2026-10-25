import type { Page } from '@playwright/test';

declare global {
  var nylzoViolations: string[] | undefined;
  var nylzoBlobTypes: string[] | undefined;
}

export async function watchViolations(page: Page): Promise<() => Promise<string[]>> {
  const errors: string[] = [];
  page.on('pageerror', (error) => {
    errors.push(`pageerror: ${error.message}`);
  });
  await page.addInitScript(() => {
    const violations: string[] = [];
    Object.defineProperty(globalThis, 'nylzoViolations', { value: violations });
    document.addEventListener('securitypolicyviolation', (event) => {
      violations.push(`${event.effectiveDirective} ${event.blockedURI}`);
    });
  });
  return async () => {
    const violations = await page.evaluate(() => globalThis.nylzoViolations ?? []);
    return [...errors, ...violations];
  };
}

export interface DownloadWatch {
  readonly names: readonly string[];
  readonly blobTypes: () => Promise<string[]>;
}

export async function watchDownloads(page: Page): Promise<DownloadWatch> {
  const names: string[] = [];
  page.on('download', (download) => {
    names.push(download.suggestedFilename());
  });
  await page.addInitScript(() => {
    const types: string[] = [];
    Object.defineProperty(globalThis, 'nylzoBlobTypes', { value: types });
    const createObjectUrl = URL.createObjectURL.bind(URL);
    URL.createObjectURL = (object: Blob | MediaSource) => {
      types.push(object instanceof Blob ? object.type : 'media-source');
      return createObjectUrl(object);
    };
  });
  return { names, blobTypes: () => page.evaluate(() => globalThis.nylzoBlobTypes ?? []) };
}
