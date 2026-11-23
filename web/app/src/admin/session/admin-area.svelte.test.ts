import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../../shared/api/api-errors.ts';
import type { Live, Overview, Range } from '../api/types.ts';
import { EMPTY_LIVE, emptyOverview } from '../dashboard/overview.ts';
import { AdminArea } from './admin-area.svelte.ts';
import type { AdminClient, AdminSession } from './client.ts';
import { passkeysSupported } from './passkey.ts';
import { KeyTypeError, SessionEndedError, UnreachableError } from './problems.ts';
import type { Problem } from './problems.ts';

vi.mock(import('./passkey.ts'), () => ({
  passkeysSupported: vi.fn<typeof import('./passkey.ts').passkeysSupported>(),
  createPasskey: vi.fn<typeof import('./passkey.ts').createPasskey>(),
  usePasskey: vi.fn<typeof import('./passkey.ts').usePasskey>(),
}));

const TOKEN = `${'A'.repeat(31)}w`;

function overviewOf(range: Range, sessionExpiresAt = 200): Overview {
  return { ...emptyOverview(range, 0), security: { passkeys: 1, lastSignIn: 0, sessionExpiresAt } };
}

function liveOf(sessionExpiresAt = 300): Live {
  return { ...EMPTY_LIVE, online: 2, sessionExpiresAt };
}

function fakeSession() {
  return {
    expiresAt: 100,
    overview: vi.fn<AdminSession['overview']>((range) => Promise.resolve(overviewOf(range))),
    live: vi.fn<AdminSession['live']>(() => Promise.resolve(liveOf())),
    signOut: vi.fn<AdminSession['signOut']>(() => Promise.resolve()),
  } satisfies AdminSession;
}

async function settle(): Promise<void> {
  await vi.advanceTimersByTimeAsync(0);
}

describe('AdminArea', () => {
  let hidden: boolean;
  let session: ReturnType<typeof fakeSession>;
  const client = {
    enrolled: vi.fn<AdminClient['enrolled']>(),
    enroll: vi.fn<AdminClient['enroll']>(),
    signIn: vi.fn<AdminClient['signIn']>(),
  };

  function newArea(): AdminArea {
    return new AdminArea({ client, now: () => Date.now(), hidden: () => hidden });
  }

  async function signedIn(): Promise<AdminArea> {
    const area = newArea();
    await area.start();
    await area.signIn();
    await settle();
    return area;
  }

  beforeEach(() => {
    vi.useFakeTimers({ now: 1_000_000 });
    vi.resetAllMocks();
    vi.stubGlobal('isSecureContext', true);
    hidden = false;
    session = fakeSession();
    vi.mocked(passkeysSupported).mockResolvedValue(true);
    client.enrolled.mockResolvedValue(true);
    client.enroll.mockResolvedValue();
    client.signIn.mockResolvedValue(session);
    history.replaceState(null, '', '/admin');
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  describe('start', () => {
    it('stops at a browser without passkeys before asking the server', async () => {
      vi.mocked(passkeysSupported).mockResolvedValue(false);
      const area = newArea();

      await area.start();

      expect(area.stage).toBe('unsupported');
      expect(client.enrolled).not.toHaveBeenCalled();
    });

    it('asks for HTTPS on an insecure address, where no browser offers passkeys', async () => {
      vi.stubGlobal('isSecureContext', false);
      const area = newArea();

      await area.start();

      expect(area.stage).toBe('insecure');
      expect(passkeysSupported).not.toHaveBeenCalled();
    });

    it('offers sign-in once a passkey is enrolled and the setup commands before', async () => {
      const area = newArea();
      await area.start();
      expect(area.stage).toBe('signIn');

      client.enrolled.mockResolvedValue(false);
      await area.start();
      expect(area.stage).toBe('unconfigured');
    });

    it('shows why the server could not be asked and clears the problem on a retry', async () => {
      client.enrolled.mockRejectedValueOnce(new UnreachableError(new TypeError('offline')));
      const area = newArea();

      await area.start();
      expect(area.stage).toBe('unavailable');
      expect(area.problem).toBe('offline');

      await area.start();
      expect(area.stage).toBe('signIn');
      expect(area.problem).toBeUndefined();
    });
  });

  describe('enrollment', () => {
    it('takes the token from the address, enrolls with it and signs in right away', async () => {
      history.replaceState(null, '', `/admin#enroll=${TOKEN}`);
      const area = newArea();

      await area.start();
      expect(area.stage).toBe('enroll');
      expect(location.hash).toBe('');
      expect(client.enrolled).not.toHaveBeenCalled();

      await area.enroll();
      expect(area.stage).toBe('signedIn');
      expect(area.notice).toBeUndefined();
      expect(area.busy).toBe(false);

      await area.enroll();
      expect(client.enroll).toHaveBeenCalledExactlyOnceWith(TOKEN);
    });

    it('keeps the enrollment notice without a problem when the browser wants a new click to sign in', async () => {
      history.replaceState(null, '', `/admin#enroll=${TOKEN}`);
      client.signIn.mockRejectedValue(new DOMException('wait', 'NotAllowedError'));
      const area = newArea();
      await area.start();

      await area.enroll();

      expect(area.stage).toBe('signIn');
      expect(area.notice).toBe('enrolled');
      expect(area.problem).toBeUndefined();
    });

    it('shows a real sign-in failure right after enrolling', async () => {
      history.replaceState(null, '', `/admin#enroll=${TOKEN}`);
      client.signIn.mockRejectedValue(new ApiError(401, 'unexpected'));
      const area = newArea();
      await area.start();

      await area.enroll();

      expect(area.notice).toBe('enrolled');
      expect(area.problem).toBe('refused');
    });

    it('keeps the token after a refused enrollment so the owner can try again', async () => {
      history.replaceState(null, '', `/admin#enroll=${TOKEN}`);
      client.enroll.mockRejectedValueOnce(new ApiError(401, 'unexpected'));
      const area = newArea();
      await area.start();

      await area.enroll();
      expect(area.stage).toBe('enroll');
      expect(area.problem).toBe('link');
      expect(area.busy).toBe(false);

      await area.enroll();
      expect(client.enroll).toHaveBeenLastCalledWith(TOKEN);
      expect(area.stage).toBe('signedIn');
    });

    it('ignores a second click while the first ceremony runs', async () => {
      history.replaceState(null, '', `/admin#enroll=${TOKEN}`);
      const area = newArea();
      await area.start();

      const first = area.enroll();
      expect(area.busy).toBe(true);
      await area.enroll();
      await area.signIn();
      await first;

      expect(client.enroll).toHaveBeenCalledOnce();
      expect(client.signIn).toHaveBeenCalledOnce();
    });
  });

  describe('sign-in', () => {
    it.each<[string, unknown, Problem]>([
      ['a passkey without a public key', new KeyTypeError(), 'key-type'],
      ['a rate limit', new ApiError(429, 'rate_limited'), 'busy'],
      ['a server that is unavailable', new ApiError(503, 'unavailable'), 'busy'],
      ['a refused passkey', new ApiError(401, 'unexpected'), 'refused'],
      ['another server error', new ApiError(500, 'internal'), 'failed'],
      ['an unreachable server', new UnreachableError(new TypeError('offline')), 'offline'],
      ['a cancelled ceremony', new DOMException('no', 'NotAllowedError'), 'cancelled'],
      ['an aborted ceremony', new DOMException('no', 'AbortError'), 'cancelled'],
      ['a passkey that is already enrolled', new DOMException('no', 'InvalidStateError'), 'already-enrolled'],
      ['a page the passkey is not for', new DOMException('no', 'SecurityError'), 'not-allowed'],
      ['an authenticator without the algorithm', new DOMException('no', 'NotSupportedError'), 'unsupported'],
      ['another browser error', new DOMException('no', 'DataError'), 'failed'],
      ['any other failure', new TypeError('broken'), 'failed'],
    ])('names %s', async (_, error, problem) => {
      client.signIn.mockRejectedValue(error);
      const area = newArea();
      await area.start();

      await area.signIn();

      expect(area.problem).toBe(problem);
      expect(area.stage).toBe('signIn');
      expect(area.busy).toBe(false);
    });

    it('loads the figures, takes the expiry the server reports and refreshes the live ones every five seconds', async () => {
      const area = await signedIn();

      expect(area.stage).toBe('signedIn');
      expect(area.overview?.range).toBe('7d');
      expect(area.live?.online).toBe(2);
      expect(area.expiresAt).toBe(300);
      expect(area.failed).toBe(false);
      expect(area.stale).toBe(false);
      expect(session.overview).toHaveBeenCalledExactlyOnceWith('7d');

      await vi.advanceTimersByTimeAsync(5000);
      expect(session.live).toHaveBeenCalledTimes(2);
      expect(session.overview).toHaveBeenCalledOnce();

      await vi.advanceTimersByTimeAsync(55_000);
      expect(session.live).toHaveBeenCalledTimes(13);
      expect(session.overview).toHaveBeenCalledTimes(2);
    });
  });

  describe('refresh', () => {
    it('waits while the page is hidden and refreshes as soon as it shows again', async () => {
      hidden = true;
      const area = await signedIn();
      expect(session.live).not.toHaveBeenCalled();

      hidden = false;
      const pending = Promise.withResolvers<Live>();
      session.live.mockReturnValueOnce(pending.promise);
      area.wake();
      hidden = true;
      pending.resolve(liveOf());
      await vi.advanceTimersByTimeAsync(60_000);
      expect(session.live).toHaveBeenCalledOnce();
      expect(area.live?.online).toBe(2);

      hidden = false;
      area.wake();
      await settle();
      expect(session.live).toHaveBeenCalledTimes(2);
    });

    it('marks the live figures stale and backs off up to a minute until they load again', async () => {
      const area = await signedIn();
      session.live.mockRejectedValue(new ApiError(500, 'internal'));

      const calls: number[] = [];
      for (const wait of [5000, 10_000, 20_000, 40_000, 60_000, 60_000]) {
        await vi.advanceTimersByTimeAsync(wait - 1);
        calls.push(session.live.mock.calls.length);
        await vi.advanceTimersByTimeAsync(1);
        calls.push(session.live.mock.calls.length);
      }
      expect(calls).toEqual([1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7]);
      expect(area.stale).toBe(true);
      expect(area.failed).toBe(true);
      expect(area.live?.online).toBe(2);

      session.live.mockResolvedValue(liveOf());
      await vi.advanceTimersByTimeAsync(60_000);
      expect(area.stale).toBe(false);
      expect(area.failed).toBe(false);
      await vi.advanceTimersByTimeAsync(5000);
      expect(session.live).toHaveBeenCalledTimes(9);
    });

    it('retries a failed overview after a growing delay while the live figures keep coming', async () => {
      session.overview.mockRejectedValueOnce(new ApiError(500, 'internal'));
      session.overview.mockRejectedValueOnce(new ApiError(500, 'internal'));
      const area = await signedIn();
      expect(area.failed).toBe(true);
      expect(area.stale).toBe(false);
      expect(area.overview).toBeUndefined();

      await vi.advanceTimersByTimeAsync(5000);
      expect(session.overview).toHaveBeenCalledOnce();
      await vi.advanceTimersByTimeAsync(5000);
      expect(session.overview).toHaveBeenCalledTimes(2);

      await vi.advanceTimersByTimeAsync(15_000);
      expect(session.overview).toHaveBeenCalledTimes(2);
      await vi.advanceTimersByTimeAsync(5000);
      expect(session.overview).toHaveBeenCalledTimes(3);
      expect(area.overview?.range).toBe('7d');
      expect(area.failed).toBe(false);
    });

    it('asks for the overview of a new range at once and ignores the old range answer that arrives late', async () => {
      const area = await signedIn();
      const late = Promise.withResolvers<Overview>();
      session.overview.mockReturnValueOnce(late.promise);
      area.chooseRange('7d');
      expect(session.overview).toHaveBeenCalledOnce();

      await vi.advanceTimersByTimeAsync(60_000);
      expect(session.overview).toHaveBeenLastCalledWith('7d');
      area.chooseRange('30d');
      late.resolve(overviewOf('7d', 999));
      await settle();

      expect(session.overview).toHaveBeenLastCalledWith('30d');
      expect(area.range).toBe('30d');
      expect(area.overview?.range).toBe('30d');
      expect(area.expiresAt).not.toBe(999);
      expect(area.failed).toBe(false);
    });

    it('ignores a failed overview of a range left during the refresh', async () => {
      const area = await signedIn();
      const late = Promise.withResolvers<Overview>();
      session.overview.mockReturnValueOnce(late.promise);
      area.chooseRange('24h');
      area.chooseRange('90d');
      late.reject(new ApiError(500, 'internal'));
      await settle();

      expect(area.overview?.range).toBe('90d');
      expect(area.failed).toBe(false);
    });

    it('ends the session the server no longer knows and offers sign-in again', async () => {
      const area = await signedIn();
      session.live.mockRejectedValue(new SessionEndedError());

      await vi.advanceTimersByTimeAsync(5000);

      expect(area.stage).toBe('signIn');
      expect(area.notice).toBe('ended');
      expect(area.overview).toBeUndefined();
      expect(area.live).toBeUndefined();
      expect(area.expiresAt).toBeUndefined();
      await vi.advanceTimersByTimeAsync(60_000);
      expect(session.live).toHaveBeenCalledTimes(2);
    });
  });

  describe('sign-out', () => {
    it('clears the figures at once and ignores answers that arrive after it', async () => {
      const area = await signedIn();
      const pending = Promise.withResolvers<Live>();
      session.live.mockReturnValueOnce(pending.promise);
      await vi.advanceTimersByTimeAsync(5000);

      await area.signOut();
      pending.resolve(liveOf(777));
      await settle();

      expect(session.signOut).toHaveBeenCalledOnce();
      expect(area.stage).toBe('signIn');
      expect(area.notice).toBe('signedOut');
      expect(area.live).toBeUndefined();
      expect(area.expiresAt).toBeUndefined();
      await vi.advanceTimersByTimeAsync(60_000);
      expect(session.live).toHaveBeenCalledTimes(2);
    });

    it('keeps the sign-out notice when the session ends during a refresh that was running', async () => {
      const area = await signedIn();
      const pending = Promise.withResolvers<Live>();
      session.live.mockReturnValueOnce(pending.promise);
      session.signOut.mockRejectedValue(new ApiError(503, 'unavailable'));
      await vi.advanceTimersByTimeAsync(5000);

      await area.signOut();
      pending.reject(new SessionEndedError());
      await settle();

      expect(area.notice).toBe('signedOut');
      expect(area.stage).toBe('signIn');
    });

    it('signs out without a session', async () => {
      const area = newArea();
      await area.start();

      await area.signOut();

      expect(area.notice).toBe('signedOut');
      expect(area.stage).toBe('signIn');
    });
  });

  it('refreshes once more after a wake that came during a refresh', async () => {
    const area = await signedIn();
    const pending = Promise.withResolvers<Live>();
    session.live.mockReturnValueOnce(pending.promise);
    await vi.advanceTimersByTimeAsync(5000);

    area.wake();
    expect(session.live).toHaveBeenCalledTimes(2);
    pending.resolve(liveOf());
    await settle();

    expect(session.live).toHaveBeenCalledTimes(3);
  });

  it('stops polling when asked', async () => {
    const area = await signedIn();

    area.stop();
    await vi.advanceTimersByTimeAsync(60_000);

    expect(session.live).toHaveBeenCalledOnce();
  });
});
