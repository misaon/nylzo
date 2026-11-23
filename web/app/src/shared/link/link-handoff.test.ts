import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { takeHandedOverFragment } from '../local-client/handoff.ts';
import { takeFragment } from './link-handoff.ts';

vi.mock(import('../local-client/handoff.ts'), () => ({
  takeHandedOverFragment: vi.fn<typeof takeHandedOverFragment>(),
}));

const replaceState = vi.fn<History['replaceState']>();

function openAt(hash: string): void {
  vi.stubGlobal('location', { hash, pathname: '/r', search: '?lang=cs' });
  vi.stubGlobal('history', { replaceState });
}

beforeEach(() => {
  vi.mocked(takeHandedOverFragment).mockReturnValue(undefined);
  replaceState.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
  globalThis.nylzoFragment = undefined;
});

describe('taking the fragment of a link', () => {
  it('takes it from the address and removes it from the history entry', () => {
    openAt('#3.3.fields');
    expect(takeFragment()).toEqual({ fragment: '#3.3.fields', handedOver: false });
    expect(replaceState).toHaveBeenCalledExactlyOnceWith(null, '', '/r?lang=cs');
  });

  it('leaves the history alone when the address has no fragment', () => {
    openAt('');
    expect(takeFragment()).toEqual({ fragment: '', handedOver: false });
    expect(replaceState).not.toHaveBeenCalled();
  });

  it('prefers the fragment the page script took before the address showed it', () => {
    openAt('#ignored');
    globalThis.nylzoFragment = () => '#3.3.early';
    expect(takeFragment()).toEqual({ fragment: '#3.3.early', handedOver: false });
    expect(replaceState).not.toHaveBeenCalled();
  });

  it('takes the fragment the local client handed over', () => {
    openAt('#ignored');
    vi.mocked(takeHandedOverFragment).mockReturnValue('3.3.handed');
    expect(takeFragment()).toEqual({ fragment: '3.3.handed', handedOver: true });
  });
});
