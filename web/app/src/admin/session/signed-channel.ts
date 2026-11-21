import { authorizationHeader, parseChallenge, PURPOSE_ADMIN, signRequest, wipeRequestKey } from '@nylzo/core';
import type { RequestKey } from '@nylzo/core';

import { ApiError } from '../../shared/api/api-errors.ts';
import { text } from '../../shared/api/responses.ts';
import type { Json } from '../../shared/api/responses.ts';
import { SessionEndedError } from './problems.ts';
import { CHALLENGE_PATH } from './transport.ts';
import type { Transport } from './transport.ts';

// The server keeps a challenge for 60 seconds; one older than this may run out on its way.
export const CHALLENGE_FRESH_MS = 45_000;

interface Held {
  readonly challenge: Uint8Array<ArrayBuffer>;
  readonly receivedAt: number;
}

export interface SignedChannelOptions {
  readonly key: RequestKey;
  readonly transport: Transport;
  readonly now: () => number;
  readonly challenge?: string | undefined;
}

export type SignedMethod = 'DELETE' | 'GET';

async function settled(promise: Promise<unknown>): Promise<void> {
  try {
    await promise;
  } catch {}
}

// Each challenge works once, so the channel sends one signed request at a time and signs the next one over the
// challenge the last answer carried.
export class SignedChannel {
  readonly #key: RequestKey;
  readonly #transport: Transport;
  readonly #now: () => number;
  #held: Held | undefined;
  #queue: Promise<unknown> = Promise.resolve();
  #ended = false;

  constructor(options: SignedChannelOptions) {
    this.#key = options.key;
    this.#transport = options.transport;
    this.#now = options.now;
    this.#keep(options.challenge);
  }

  get ended(): boolean {
    return this.#ended;
  }

  async request(method: SignedMethod, path: string): Promise<Json> {
    const sent = this.#queue.then(async () => this.#send(method, path));
    this.#queue = settled(sent);
    return sent;
  }

  end(): void {
    this.#ended = true;
    this.#held = undefined;
    wipeRequestKey(this.#key);
  }

  #keep(challenge: string | undefined): void {
    this.#held = undefined;
    if (challenge === undefined) {
      return;
    }
    try {
      this.#held = { challenge: parseChallenge(challenge), receivedAt: this.#now() };
    } catch {}
  }

  async #fresh(): Promise<Uint8Array<ArrayBuffer>> {
    return parseChallenge(text(await this.#transport.send('GET', CHALLENGE_PATH), 'challenge'));
  }

  async #signed(method: SignedMethod, path: string, challenge: Uint8Array<ArrayBuffer>): Promise<Json> {
    const signature = await signRequest(this.#key, { purpose: PURPOSE_ADMIN, method, path, challenge });
    const answer = await this.#transport.send(method, path, {
      headers: { Authorization: authorizationHeader(signature) },
    });
    const next = answer['challenge'];
    this.#keep(typeof next === 'string' ? next : undefined);
    return answer;
  }

  // The server answers 401 for a session that ended and for a challenge that ran out alike. A challenge that was
  // kept may have run out while the machine slept, so only a 401 over a fresh one ends the session.
  async #send(method: SignedMethod, path: string): Promise<Json> {
    if (this.#ended) {
      throw new SessionEndedError();
    }
    const held = this.#held;
    this.#held = undefined;
    try {
      if (held && this.#now() - held.receivedAt < CHALLENGE_FRESH_MS) {
        try {
          return await this.#signed(method, path, held.challenge);
        } catch (error) {
          if (!(error instanceof ApiError && error.status === 401)) {
            throw error;
          }
        }
      }
      return await this.#signed(method, path, await this.#fresh());
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        this.end();
        throw new SessionEndedError();
      }
      throw error;
    }
  }
}
