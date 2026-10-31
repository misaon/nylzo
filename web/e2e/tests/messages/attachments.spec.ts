import { gzipSync } from 'node:zlib';

import { expect, test } from '../support/fixtures.ts';
import { createMessageThroughApi } from '../support/messages.ts';
import { expectMessageShown, notInert, showMessage } from '../support/pages.ts';
import { watchDownloads } from '../support/watchers.ts';

const RISKY_FILES = [
  {
    name: 'drawing.svg',
    shown: 'drawing.svg',
    extension: 'svg',
    mimeType: 'image/svg+xml',
    body: '<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>alert(2)</script></svg>',
  },
  {
    name: 'page.html',
    shown: 'page.html',
    extension: 'html',
    mimeType: 'text/html',
    body: '<!doctype html><script>alert(3)</script>',
  },
  {
    name: 'invoice\u{202E}fdp.exe',
    shown: 'invoicefdp.exe',
    extension: 'exe',
    mimeType: 'application/pdf',
    body: 'MZ',
  },
];

function malformedCompressedAttachment(
  problem: 'checksum' | 'expansion' | 'trailing' | 'multiple frames',
): Uint8Array<ArrayBuffer> {
  const original = Buffer.from('row,1,alpha\n'.repeat(20_000));
  let encoded = gzipSync(original);
  let size = original.length;
  switch (problem) {
    case 'checksum': {
      encoded[encoded.length - 8] = (encoded.at(-8) ?? 0) ^ 1;
      break;
    }
    case 'expansion': {
      size = 1024;
      break;
    }
    case 'trailing': {
      encoded = Buffer.concat([encoded, Buffer.from([1])]);
      break;
    }
    case 'multiple frames': {
      encoded = Buffer.concat([encoded, encoded]);
      size *= 2;
      break;
    }
  }
  const meta = Buffer.from(
    JSON.stringify({
      v: 2,
      text: 'must not be displayed',
      file: { name: 'rows.csv', size, encoding: 'gzip', encoded_size: encoded.length },
    }),
  );
  const plaintext = new Uint8Array(65_536 - 15 - 16);
  new DataView(plaintext.buffer).setUint32(0, meta.length);
  plaintext.set(meta, 4);
  plaintext.set(encoded, 4 + meta.length);
  return plaintext;
}

for (const problem of ['checksum', 'expansion', 'trailing', 'multiple frames'] as const) {
  test(
    `a compressed attachment with ${problem} is rejected before display or download`,
    { tag: '@cross-browser' },
    async ({ page }) => {
      const plaintext = malformedCompressedAttachment(problem);
      const message = await createMessageThroughApi({ plaintext });
      await showMessage(page, message.link);
      await expect(page.getByText('The message cannot be opened safely', { exact: true })).toBeVisible();
      await expect(page.locator('pre')).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Download attachment' })).toHaveCount(0);
    },
  );
}

for (const file of RISKY_FILES) {
  test(
    `a risky attachment ${file.shown} downloads only after a warning, once per double click, under its sanitised name and as application/octet-stream (AK13, D35)`,
    { tag: '@cross-browser' },
    async ({ page }) => {
      const message = await createMessageThroughApi({
        file: { name: file.name, mimeType: file.mimeType, buffer: Buffer.from(file.body) },
      });
      const downloads = await watchDownloads(page);
      await showMessage(page, message.link);
      await expectMessageShown(page);
      await expect(page.locator('bdi')).toHaveText(file.shown);
      const readPage = page.url();
      const download = page.getByRole('button', { name: 'Download attachment' });
      const cancel = page.getByRole('button', { name: 'Do not download' });
      const warning = notInert(page, page.getByRole('alert')).filter({ hasText: /can run code/ });

      await download.dblclick();
      await expect(warning).toHaveCount(1);
      await expect(page.getByText(`File extension: .${file.extension}`)).toBeVisible();
      await cancel.dispatchEvent('click', { detail: 2 });
      await expect(warning).toHaveCount(1);
      await cancel.click();
      await expect(warning).toHaveCount(0);
      await expect(download).toBeFocused();
      expect(downloads.names).toEqual([]);

      const downloadAnyway = page.getByRole('button', { name: 'Download anyway' });
      await download.click();
      await expect(cancel).toBeFocused();
      const first = page.waitForEvent('download');
      await downloadAnyway.click();
      const firstSaved = await first;
      expect(firstSaved.suggestedFilename()).toBe(file.shown);
      await expect(download).toBeFocused();

      await download.click();
      const second = page.waitForEvent('download');
      await downloadAnyway.dblclick();
      const secondSaved = await second;
      expect(secondSaved.suggestedFilename()).toBe(file.shown);
      await expect(warning).toHaveCount(0);
      expect(await downloads.blobTypes()).toEqual(['application/octet-stream', 'application/octet-stream']);
      expect(downloads.names).toEqual([file.shown, file.shown]);
      expect(page.url()).toBe(readPage);
      expect(page.context().pages()).toHaveLength(1);
    },
  );
}

test(
  'a safe attachment downloads at once, once per double click, as application/octet-stream (AK13, D35)',
  { tag: '@cross-browser' },
  async ({ page }) => {
    const message = await createMessageThroughApi({
      file: { name: 'report.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4\n%nylzo\n') },
    });
    const downloads = await watchDownloads(page);
    await showMessage(page, message.link);
    await expectMessageShown(page);
    await expect(page.locator('bdi')).toHaveText('report.pdf');
    const readPage = page.url();
    const saving = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download attachment' }).dblclick();
    const saved = await saving;
    expect(saved.suggestedFilename()).toBe('report.pdf');
    await expect(page.getByText(/can run code/)).toHaveCount(0);
    expect(await downloads.blobTypes()).toEqual(['application/octet-stream']);
    expect(downloads.names).toEqual(['report.pdf']);
    expect(page.url()).toBe(readPage);
    expect(page.context().pages()).toHaveLength(1);
  },
);
