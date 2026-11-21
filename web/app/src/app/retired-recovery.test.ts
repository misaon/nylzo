// @vitest-environment happy-dom
import { IDBFactory } from 'fake-indexeddb';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { forgetRetiredRecovery } from './retired-recovery.ts';

describe('retired recovery cleanup', () => {
  afterEach(() => {
    sessionStorage.clear();
  });

  it('removes every retired key pointer and per-message scrypt mark while preserving unrelated session state', () => {
    sessionStorage.setItem('nylzo:link:first', 'synthetic');
    sessionStorage.setItem('unrelated', 'keep');
    sessionStorage.setItem('nylzo:link:second', 'synthetic');
    sessionStorage.setItem('nylzo:kdf:AAAAAAAAAAAAAAAAAAAAAA', '1');
    const databases = new IDBFactory();
    const deleteDatabase = vi.spyOn(databases, 'deleteDatabase');
    forgetRetiredRecovery(() => sessionStorage, databases);
    expect(sessionStorage.length).toBe(1);
    expect(sessionStorage.getItem('unrelated')).toBe('keep');
    expect(deleteDatabase).toHaveBeenCalledExactlyOnceWith('nylzo-resume');
  });

  it('continues startup when storage access or legacy deletion is refused', () => {
    const databases = new IDBFactory();
    const deleteDatabase = vi.spyOn(databases, 'deleteDatabase').mockImplementation(() => {
      throw new Error('blocked');
    });
    expect(() =>
      forgetRetiredRecovery(() => {
        throw new Error('denied');
      }, databases),
    ).not.toThrow();
    expect(deleteDatabase).toHaveBeenCalledOnce();
    expect(() => forgetRetiredRecovery(() => sessionStorage, undefined)).not.toThrow();
  });
});
