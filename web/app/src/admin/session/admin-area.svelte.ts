import type { Live, Overview, Range } from '../api/types.ts';
import type { AdminClient, AdminSession } from './client.ts';
import { takeEnrollmentToken } from './enrollment-link.ts';
import { passkeysSupported } from './passkey.ts';
import { problemOf, SessionEndedError } from './problems.ts';
import type { Problem } from './problems.ts';

export type Stage = 'enroll' | 'loading' | 'signedIn' | 'signIn' | 'unavailable' | 'unconfigured' | 'unsupported';

export type Notice = 'ended' | 'enrolled' | 'signedOut';

const LIVE_INTERVAL_MS = 5000;
const OVERVIEW_INTERVAL_MS = 60_000;
const LONGEST_RETRY_MS = 60_000;

function retryDelay(failures: number): number {
  return Math.min(LIVE_INTERVAL_MS * 2 ** failures, LONGEST_RETRY_MS);
}

interface Environment {
  readonly client: AdminClient;
  readonly now: () => number;
  readonly hidden: () => boolean;
}

// Reports whether the request worked; only the end of the session is an error to its caller.
async function load<Value>(request: () => Promise<Value>, apply: (value: Value) => void): Promise<boolean> {
  try {
    apply(await request());
    return true;
  } catch (error) {
    if (error instanceof SessionEndedError) {
      throw error;
    }
    return false;
  }
}

export class AdminArea {
  stage: Stage = $state('loading');
  busy = $state(false);
  problem: Problem | undefined = $state();
  notice: Notice | undefined = $state();
  range: Range = $state('7d');
  overview: Overview | undefined = $state.raw();
  live: Live | undefined = $state.raw();
  failed = $state(false);
  // The live figures could not be refreshed, so what is shown is not the present any more.
  stale = $state(false);
  expiresAt: number | undefined = $state();

  readonly #environment: Environment;
  #token: string | undefined;
  #session: AdminSession | undefined;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #refreshing = false;
  #again = false;
  #interval = LIVE_INTERVAL_MS;
  #overviewAt = 0;
  #overviewFailures = 0;
  #overviewRetryAt = 0;

  constructor(environment: Environment) {
    this.#environment = environment;
  }

  async start(): Promise<void> {
    this.#token ??= takeEnrollmentToken({ location, history });
    this.stage = 'loading';
    this.problem = undefined;
    if (!(await passkeysSupported())) {
      this.stage = 'unsupported';
      return;
    }
    if (this.#token !== undefined) {
      this.stage = 'enroll';
      return;
    }
    try {
      this.stage = (await this.#environment.client.enrolled()) ? 'signIn' : 'unconfigured';
    } catch (error) {
      this.problem = problemOf(error, 'failed');
      this.stage = 'unavailable';
    }
  }

  async enroll(): Promise<void> {
    const token = this.#token;
    if (token === undefined || this.busy) {
      return;
    }
    this.busy = true;
    this.problem = undefined;
    try {
      await this.#environment.client.enroll(token);
    } catch (error) {
      this.problem = problemOf(error, 'link');
      this.busy = false;
      return;
    }
    this.#token = undefined;
    this.stage = 'signIn';
    this.notice = 'enrolled';
    this.busy = false;
    await this.signIn({ quiet: true });
  }

  // Right after a passkey was created, a browser may want a new click before it uses it, which is no problem to show.
  async signIn({ quiet = false } = {}): Promise<void> {
    if (this.busy) {
      return;
    }
    this.busy = true;
    this.problem = undefined;
    try {
      const session = await this.#environment.client.signIn();
      this.#session = session;
      this.expiresAt = session.expiresAt;
      this.notice = undefined;
      this.stage = 'signedIn';
      this.wake();
    } catch (error) {
      const problem = problemOf(error, 'refused');
      if (!quiet || problem !== 'cancelled') {
        this.problem = problem;
      }
    } finally {
      this.busy = false;
    }
  }

  async signOut(): Promise<void> {
    const session = this.#session;
    this.#leave('signedOut');
    try {
      await session?.signOut();
    } catch {}
  }

  chooseRange(range: Range): void {
    if (range === this.range) {
      return;
    }
    this.range = range;
    this.#overviewRetryAt = 0;
    this.wake();
  }

  readonly wake = (): void => {
    clearTimeout(this.#timer);
    this.#timer = undefined;
    if (this.#refreshing) {
      this.#again = true;
      return;
    }
    if (this.#session === undefined || this.#environment.hidden()) {
      return;
    }
    void this.#refresh(this.#session);
  };

  stop(): void {
    clearTimeout(this.#timer);
    this.#timer = undefined;
  }

  #leave(notice: Notice): void {
    this.stop();
    this.#session = undefined;
    this.overview = undefined;
    this.live = undefined;
    this.failed = false;
    this.stale = false;
    this.#interval = LIVE_INTERVAL_MS;
    this.#overviewFailures = 0;
    this.#overviewRetryAt = 0;
    this.expiresAt = undefined;
    this.notice = notice;
    this.stage = 'signIn';
  }

  async #refresh(session: AdminSession): Promise<void> {
    this.#refreshing = true;
    this.#again = false;
    const range = this.range;
    const now = this.#environment.now();
    const overviewDue =
      now >= this.#overviewRetryAt &&
      (this.overview?.range !== range || now - this.#overviewAt >= OVERVIEW_INTERVAL_MS);
    let loaded: readonly boolean[] = [];
    try {
      loaded = await Promise.all([
        overviewDue
          ? load(
              async () => session.overview(range),
              (overview) => {
                if (this.#session === session && this.range === overview.range) {
                  this.overview = overview;
                  this.#overviewAt = this.#environment.now();
                  this.expiresAt = overview.security.sessionExpiresAt;
                }
              },
            )
          : true,
        load(
          async () => session.live(),
          (live) => {
            if (this.#session === session) {
              this.live = live;
              this.expiresAt = live.sessionExpiresAt;
            }
          },
        ),
      ]);
    } catch {
      if (this.#session === session) {
        this.#leave('ended');
      }
    } finally {
      this.#refreshing = false;
    }
    const current = this.#session === session;
    if (current) {
      const [overviewLoaded = false, liveLoaded = false] = loaded;
      // A range chosen during the refresh asks for its own overview; the old range's answer sets no backoff.
      if (overviewDue && range === this.range) {
        this.#overviewFailures = overviewLoaded ? 0 : this.#overviewFailures + 1;
        this.#overviewRetryAt = overviewLoaded ? 0 : this.#environment.now() + retryDelay(this.#overviewFailures);
      }
      this.failed = this.#overviewFailures > 0 || !liveLoaded;
      this.stale = !liveLoaded;
      this.#interval = liveLoaded ? LIVE_INTERVAL_MS : Math.min(this.#interval * 2, LONGEST_RETRY_MS);
    }
    if (this.#again) {
      this.wake();
    } else if (current && !this.#environment.hidden()) {
      this.#timer = setTimeout(this.wake, this.#interval);
    }
  }
}
