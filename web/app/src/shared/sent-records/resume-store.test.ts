import { describe, expect, it, vi } from 'vitest';

import { browserResumeStore, discardPlaintextRecovery, purgeResumeRecords } from './resume-store.ts';

const ID = 'AAAAAAAAAAAAAAAAAAAAAA';
const TOKEN = 'a'.repeat(43);

function sessionStorageOf(values: Record<string, string>): Storage {
  const map = new Map(Object.entries(values));
  return {
    get length() {
      return map.size;
    },
    key: (index) => {
      let current = 0;
      for (const key of map.keys()) {
        if (current++ === index) {
          return key;
        }
      }
      return null;
    },
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => {
      map.set(key, value);
    },
    removeItem: (key) => {
      map.delete(key);
    },
    clear: () => map.clear(),
  };
}

describe('encrypted reading recovery', () => {
  it('removes legacy plaintext fragments and their leases while preserving opaque pointers and preferences', () => {
    const session = sessionStorageOf({
      'nylzo:link:old': '#1.0.secret',
      'nylzo:lease:old': 'lease-old',
      'nylzo:link:new': TOKEN,
      'nylzo:lease:new': 'lease-new',
      language: 'en',
    });
    discardPlaintextRecovery(session);
    expect(session.getItem('nylzo:link:old')).toBeNull();
    expect(session.getItem('nylzo:lease:old')).toBeNull();
    expect(session.getItem('nylzo:link:new')).toBe(TOKEN);
    expect(session.getItem('nylzo:lease:new')).toBe('lease-new');
    expect(session.getItem('language')).toBe('en');
  });

  it('neither creates a database during cleanup nor opens one for a plaintext recovery token', async () => {
    const open = vi.fn(() => {
      throw new Error('must not open a database');
    });
    const factory = { databases: () => Promise.resolve([]), open } as unknown as IDBFactory;
    await purgeResumeRecords(factory, Date.now());
    expect(await browserResumeStore(factory).get(ID, '#1.0.secret', Date.now())).toBeUndefined();
    await browserResumeStore(factory).remove('#1.0.secret');
    expect(open).not.toHaveBeenCalled();
  });

  it('rejects storage errors instead of returning or persisting the fragment in plaintext', async () => {
    const factory = {
      open: () => {
        throw new DOMException('blocked', 'SecurityError');
      },
    } as unknown as IDBFactory;
    await expect(browserResumeStore(factory).put(ID, '#1.0.secret', Date.now())).rejects.toMatchObject({
      name: 'SecurityError',
    });
  });
});
