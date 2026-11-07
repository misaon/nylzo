// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';

import { forgetRetiredRecovery } from './retired-recovery.ts';

describe('retired recovery cleanup', () => {
  afterEach(() => {
    sessionStorage.clear();
  });

  it('removes every retired key pointer while preserving unrelated session state', () => {
    sessionStorage.setItem('nylzo:link:first', 'synthetic');
    sessionStorage.setItem('unrelated', 'keep');
    sessionStorage.setItem('nylzo:link:second', 'synthetic');
    const deleteDatabase = vi.fn<(...arguments_: unknown[]) => void>();
    forgetRetiredRecovery(() => sessionStorage, { deleteDatabase } as unknown as IDBFactory);
    expect(sessionStorage.length).toBe(1);
    expect(sessionStorage.getItem('unrelated')).toBe('keep');
    expect(deleteDatabase).toHaveBeenCalledExactlyOnceWith('nylzo-resume');
  });

  it('continues startup when storage access or legacy deletion is refused', () => {
    const deleteDatabase = vi.fn<(...arguments_: unknown[]) => never>(() => {
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
