// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import Admin from './Admin.svelte';

const startup = vi.hoisted(() => ({
  mount: vi.fn<typeof import('svelte').mount>(),
  loadFont: vi.fn<(font: string, sample: string) => Promise<unknown>>(),
}));

vi.mock('svelte', async (original) => ({ ...(await original<typeof import('svelte')>()), mount: startup.mount }));
vi.mock(import('./Admin.svelte'), () => ({ default: vi.fn<typeof import('./Admin.svelte').default>() }));

async function boot(languages: readonly string[]): Promise<void> {
  Object.defineProperty(navigator, 'languages', { configurable: true, value: languages });
  vi.resetModules();
  await import('./main.ts');
}

function ready(): string | undefined {
  return document.documentElement.dataset['ready'];
}

describe('admin startup', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    startup.loadFont.mockResolvedValue([]);
    Object.defineProperty(document, 'fonts', { configurable: true, value: { load: startup.loadFont } });
  });

  afterEach(() => {
    vi.useRealTimers();
    Reflect.deleteProperty(document, 'fonts');
    Reflect.deleteProperty(navigator, 'languages');
    for (const attribute of ['data-fonts', 'data-ready']) {
      document.documentElement.removeAttribute(attribute);
    }
    document.documentElement.lang = '';
  });

  it('mounts the admin area in English once its fonts have loaded', async () => {
    await boot(['en-GB', 'cs']);
    await vi.waitFor(() => {
      expect(ready()).toBe('');
    });

    expect(document.documentElement.lang).toBe('en');
    expect(document.documentElement.dataset['fonts']).toBe('');
    expect(startup.loadFont.mock.calls.map(([font]) => font)).toEqual([
      '300 1em Sora',
      '400 1em Geist',
      '500 1em Geist',
    ]);
    expect(startup.mount).toHaveBeenCalledExactlyOnceWith(Admin, {
      target: document.body,
      props: { language: 'en' },
    });
  });

  it('loads the Czech glyphs for a Czech browser', async () => {
    await boot(['cs-CZ']);
    await vi.waitFor(() => {
      expect(ready()).toBe('');
    });

    expect(document.documentElement.lang).toBe('cs');
    expect(new Set(startup.loadFont.mock.calls.map(([, sample]) => sample))).toEqual(new Set(['Aáčř']));
    expect(startup.mount).toHaveBeenCalledExactlyOnceWith(Admin, {
      target: document.body,
      props: { language: 'cs' },
    });
  });

  it('starts with fallback fonts when a font fails to load', async () => {
    startup.loadFont.mockRejectedValue(new DOMException('blocked', 'NetworkError'));
    await boot(['en']);
    await vi.waitFor(() => {
      expect(ready()).toBe('');
    });

    expect(startup.mount).toHaveBeenCalledOnce();
  });

  it('waits at most one and a half seconds for slow fonts', async () => {
    startup.loadFont.mockReturnValue(Promise.withResolvers().promise);
    await boot(['en']);

    await vi.advanceTimersByTimeAsync(1499);
    expect(startup.mount).not.toHaveBeenCalled();
    expect(document.documentElement.dataset['fonts']).toBeUndefined();

    await vi.advanceTimersByTimeAsync(1);
    expect(startup.mount).toHaveBeenCalledOnce();
    expect(ready()).toBe('');
  });
});
