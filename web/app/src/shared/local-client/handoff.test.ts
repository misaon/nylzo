// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

describe('verified local client handoff', () => {
  const receive = vi.fn<typeof fetch>();
  beforeEach(() => {
    vi.resetModules();
    vi.resetAllMocks();
    vi.stubGlobal('fetch', receive);
    history.replaceState(null, '', '/open#synthetic-token');
  });
  afterEach(() => vi.unstubAllGlobals());

  it('exchanges a one-time token without leaving it or the message key in history', async () => {
    receive.mockResolvedValue(Response.json({ path: '/r', fragment: '2.0.synthetic.key.share' }));
    const { takeHandoff, takeHandedOverFragment } = await import('./handoff.ts');
    await takeHandoff();
    expect(receive).toHaveBeenCalledExactlyOnceWith('/open', {
      method: 'POST',
      body: 'synthetic-token',
      cache: 'no-store',
    });
    expect(location.pathname).toBe('/r');
    expect(location.hash).toBe('');
    expect(takeHandedOverFragment()).toBe('#2.0.synthetic.key.share');
    expect(takeHandedOverFragment()).toBeUndefined();
  });

  it.each([
    null,
    {},
    { path: 'https://untrusted.test/', fragment: 'secret' },
    { path: '/r?key=secret', fragment: 'secret' },
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

  it.each(['network', 'status', 'json'])('fails closed on a %s error and still clears the token', async (failure) => {
    receive.mockResolvedValue(new Response('not json', { status: failure === 'status' ? 410 : 200 }));
    if (failure === 'network') {
      receive.mockRejectedValue(new Error('offline'));
    }
    const { takeHandoff, takeHandedOverFragment } = await import('./handoff.ts');
    await takeHandoff();
    expect(location.hash).toBe('');
    expect(takeHandedOverFragment()).toBeUndefined();
  });

  it('does not send a request without a token or outside the handoff route', async () => {
    const { takeHandoff } = await import('./handoff.ts');
    history.replaceState(null, '', '/open');
    await takeHandoff();
    history.replaceState(null, '', '/r#message-key');
    await takeHandoff();
    expect(receive).not.toHaveBeenCalled();
    expect(location.hash).toBe('#message-key');
  });
});
