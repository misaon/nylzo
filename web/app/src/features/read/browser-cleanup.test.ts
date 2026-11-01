// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';

import { forgetRetiredRecovery } from './retired-recovery.ts';
import { saveFile } from './save-file.ts';

describe('browser data cleanup', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    vi.useRealTimers();
    sessionStorage.clear();
  });

  it('downloads only sanitized filenames, removes the temporary anchor and revokes its blob URL', () => {
    vi.useFakeTimers();
    const createObjectURL = vi.fn().mockReturnValue('blob:synthetic');
    const revokeObjectURL = vi.fn();
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

  it('removes every retired key pointer while preserving unrelated session state', () => {
    sessionStorage.setItem('nylzo:link:first', 'synthetic');
    sessionStorage.setItem('unrelated', 'keep');
    sessionStorage.setItem('nylzo:link:second', 'synthetic');
    const deleteDatabase = vi.fn();
    forgetRetiredRecovery(() => sessionStorage, { deleteDatabase } as unknown as IDBFactory);
    expect(sessionStorage.length).toBe(1);
    expect(sessionStorage.getItem('unrelated')).toBe('keep');
    expect(deleteDatabase).toHaveBeenCalledExactlyOnceWith('nylzo-resume');
  });

  it('continues startup when storage access or legacy deletion is refused', () => {
    const deleteDatabase = vi.fn(() => {
      throw new Error('blocked');
    });
    expect(() =>
      forgetRetiredRecovery(
        () => {
          throw new Error('denied');
        },
        { deleteDatabase } as unknown as IDBFactory,
      ),
    ).not.toThrow();
    expect(deleteDatabase).toHaveBeenCalledOnce();
    expect(() => forgetRetiredRecovery(() => sessionStorage, undefined)).not.toThrow();
  });
});
