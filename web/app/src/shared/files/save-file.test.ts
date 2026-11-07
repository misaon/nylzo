// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';

import { saveFile } from './save-file.ts';

describe('file download', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('downloads only sanitized filenames, removes the temporary anchor and revokes its blob URL', () => {
    vi.useFakeTimers();
    const createObjectURL = vi.fn<typeof URL.createObjectURL>().mockReturnValue('blob:synthetic');
    const revokeObjectURL = vi.fn<(...arguments_: unknown[]) => void>();
    vi.stubGlobal('URL', { createObjectURL, revokeObjectURL });
    let downloaded: { name: string; rel: string; connected: boolean } | undefined;
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      downloaded = { name: this.download, rel: this.rel, connected: this.isConnected };
    });
    saveFile(new Uint8Array([1, 2]), '../synthetic\u{202E}.exe');
    expect(downloaded).toEqual({ name: '_synthetic.exe', rel: 'noopener', connected: true });
    expect(document.querySelector('a')).toBeNull();
    expect(createObjectURL).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'application/octet-stream', size: 2 }),
    );
    expect(revokeObjectURL).not.toHaveBeenCalled();
    vi.advanceTimersByTime(60_000);
    expect(revokeObjectURL).toHaveBeenCalledExactlyOnceWith('blob:synthetic');
  });
});
