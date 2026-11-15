import { afterEach, expect, it, vi } from 'vitest';

import { readerSession } from './reader-session.ts';

afterEach(() => {
  vi.unstubAllGlobals();
});

function blocked(): never {
  throw new DOMException('blocked', 'SecurityError');
}

it('reads blocked session storage as empty and drops writes', () => {
  vi.stubGlobal('document', { querySelector: () => null });
  vi.stubGlobal('location', { origin: 'https://nylzo.test' });
  vi.stubGlobal('sessionStorage', { getItem: blocked, setItem: blocked, removeItem: blocked });
  const session = readerSession();
  expect(session.getItem('nylzo:lease:r')).toBeNull();
  expect(() => {
    session.setItem('nylzo:lease:r', 'lease');
    session.removeItem('nylzo:lease:r');
  }).not.toThrow();
});
