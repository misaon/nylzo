import { encodeBase64url, newRequestKey, wipeRequestKey } from '@nylzo/core';

import { ResponseError } from '../../shared/api/api-errors.ts';
import { integer } from '../../shared/api/responses.ts';
import type { Live, Overview, Range } from '../api/types.ts';
import { liveQuery, mergeLive } from './live-update.ts';
import type { HeldLive } from './live-update.ts';
import { parseLive, parseOverview } from './parse.ts';
import { createPasskey, usePasskey } from './passkey.ts';
import { SignedChannel } from './signed-channel.ts';
import { ADMIN_PATH, createTransport } from './transport.ts';
import type { Fetch, Transport } from './transport.ts';

export interface AdminSession {
  readonly expiresAt: number;
  readonly overview: (range: Range) => Promise<Overview>;
  readonly live: () => Promise<Live>;
  readonly signOut: () => Promise<void>;
}

export interface AdminClient {
  readonly enrolled: () => Promise<boolean>;
  readonly enroll: (token: string) => Promise<void>;
  readonly signIn: () => Promise<AdminSession>;
}

// The page asks for the live figures every five seconds, so it sends what it holds and the server answers only what
// changed.
function openSession(channel: SignedChannel, expiresAt: number): AdminSession {
  let held: HeldLive | undefined;
  return {
    expiresAt,
    overview: async (range) => parseOverview(await channel.request('GET', `${ADMIN_PATH}/overview?range=${range}`)),
    async live() {
      const update = parseLive(await channel.request('GET', `${ADMIN_PATH}/live${liveQuery(held)}`));
      held = mergeLive(held, update);
      return held.live;
    },
    async signOut() {
      try {
        await channel.request('DELETE', `${ADMIN_PATH}/session`);
      } finally {
        channel.end();
      }
    },
  };
}

// The age of a challenge is measured on a clock that never steps back, so a corrected wall clock cannot make a stale
// challenge look fresh.
export function createAdminClient(fetchResource?: Fetch, now: () => number = () => performance.now()): AdminClient {
  const transport: Transport = createTransport(fetchResource);
  return {
    async enrolled() {
      const answer = await transport.send('GET', ADMIN_PATH);
      if (typeof answer['enrolled'] !== 'boolean') {
        throw new ResponseError('field enrolled is not a boolean');
      }
      return answer['enrolled'];
    },

    async enroll(token) {
      const options = await transport.send('POST', `${ADMIN_PATH}/enrollment/options`, { body: { token } });
      const registration = await createPasskey(options);
      await transport.send('POST', `${ADMIN_PATH}/enrollment`, { body: { token, ...registration } });
    },

    async signIn() {
      const key = await newRequestKey();
      try {
        const sessionKey = encodeBase64url(key.publicKey);
        const options = await transport.send('POST', `${ADMIN_PATH}/session/options`, { body: { key: sessionKey } });
        const assertion = await usePasskey(options);
        const answer = await transport.send('POST', `${ADMIN_PATH}/session`, {
          body: { key: sessionKey, ...assertion },
        });
        const challenge = answer['challenge'];
        const channel = new SignedChannel({
          key,
          transport,
          now,
          challenge: typeof challenge === 'string' ? challenge : undefined,
        });
        return openSession(channel, integer(answer, 'expires_at'));
      } catch (error) {
        wipeRequestKey(key);
        throw error;
      }
    },
  };
}
