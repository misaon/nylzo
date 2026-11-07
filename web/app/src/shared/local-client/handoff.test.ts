// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const TOKEN = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';

function servedFrom(instance: string): void {
  document.head.replaceChildren();
  const meta = document.createElement('meta');
  meta.name = 'nylzo-instance';
  meta.content = instance;
  document.head.append(meta);
}

describe('verified local client handoff', () => {
  const receive = vi.fn<typeof fetch>();
  beforeEach(() => {
    vi.resetModules();
    vi.resetAllMocks();
    vi.stubGlobal('fetch', receive);
    servedFrom('https://nylzo.example');
    history.replaceState(null, '', `/open#${TOKEN}`);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    document.head.replaceChildren();
  });

  it('exchanges a one-time token without leaving it or the message key in history', async () => {
    receive.mockResolvedValue(Response.json({ path: '/r', fragment: '2.0.synthetic.key.share' }));
    const { takeHandoff, takeHandedOverFragment } = await import('./handoff.ts');
    await takeHandoff();
    expect(receive).toHaveBeenCalledExactlyOnceWith(
      '/open',
      expect.objectContaining({
        method: 'POST',
        body: TOKEN,
        cache: 'no-store',
        credentials: 'omit',
        redirect: 'error',
      }),
    );
    expect(location.pathname).toBe('/r');
    expect(location.hash).toBe('');
    expect(takeHandedOverFragment()).toBe('#2.0.synthetic.key.share');
    expect(takeHandedOverFragment()).toBeUndefined();
  });

  it('takes the token from the fragment guard, which has already cleared the address', async () => {
    receive.mockResolvedValue(Response.json({ path: '/r', fragment: '2.0.synthetic.key.share' }));
    history.replaceState(null, '', '/open');
    vi.stubGlobal('nylzoFragment', () => `#${TOKEN}`);
    const { takeHandoff, takeHandedOverFragment } = await import('./handoff.ts');
    await takeHandoff();
    expect(receive).toHaveBeenCalledOnce();
    expect(takeHandedOverFragment()).toBe('#2.0.synthetic.key.share');
  });

  it.each([
    null,
    {},
    { path: 'https://untrusted.test/', fragment: 'secret' },
    { path: '/r?key=secret', fragment: 'secret' },
    { path: '/sent', fragment: 'secret' },
    { path: '/r', fragment: '#secret' },
    { path: '/r', fragment: 1 },
  ])('rejects malformed handoffs without navigation: %j', async (body) => {
    receive.mockResolvedValue(Response.json(body));
    const { takeHandoff, takeHandedOverFragment } = await import('./handoff.ts');
    await takeHandoff();
    expect(location.pathname).toBe('/open');
    expect(location.hash).toBe('');
    expect(takeHandedOverFragment()).toBeUndefined();
  });

  it.each(['network', 'status', 'json', 'size'])(
    'fails closed on a %s error and still clears the token',
    async (failure) => {
      const body = failure === 'size' ? JSON.stringify({ path: '/r', fragment: 'a'.repeat(2000) }) : 'not json';
      receive.mockResolvedValue(new Response(body, { status: failure === 'status' ? 410 : 200 }));
      if (failure === 'network') {
        receive.mockRejectedValue(new Error('offline'));
      }
      const { takeHandoff, takeHandedOverFragment } = await import('./handoff.ts');
      await takeHandoff();
      expect(location.hash).toBe('');
      expect(takeHandedOverFragment()).toBeUndefined();
    },
  );

  it.each([
    ['without a token', '/open', 'https://nylzo.example'],
    ['outside the handoff route', `/r#${TOKEN}`, 'https://nylzo.example'],
    ['on the instance itself, where the fragment may be a link key', '/open#2.0.id.key.share', location.origin],
    ['with a fragment that is no token', '/open#2.0.id.key.share', 'https://nylzo.example'],
  ])('sends nothing %s', async (_case, address, instance) => {
    servedFrom(instance);
    history.replaceState(null, '', address);
    const { takeHandoff } = await import('./handoff.ts');
    await takeHandoff();
    expect(receive).not.toHaveBeenCalled();
    expect(location.hash).toBe(address.startsWith('/open') ? '' : `#${TOKEN}`);
  });
});
