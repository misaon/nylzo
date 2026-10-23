import { beforeEach, describe, expect, it, vi } from 'vitest';

import { createMessage, type CreateDeps } from '../src/lib/create.ts';
import { createReader } from '../src/lib/read.ts';
import { reportLink } from '../src/lib/report.ts';
import { fakeApi, type FakeApi } from './fake-api.ts';

const { captured } = vi.hoisted(() => ({ captured: [] as Uint8Array[] }));

vi.mock('@nylzo/core', async (importOriginal) => {
  const core = await importOriginal<typeof import('@nylzo/core')>();
  const keep = <T extends Uint8Array>(bytes: T): T => {
    captured.push(bytes);
    return bytes;
  };
  return {
    ...core,
    newSecret: () => keep(core.newSecret()),
    parseLink: (raw: string) => {
      const link = core.parseLink(raw);
      keep(link.s);
      return link;
    },
    parseFragment: (text: string) => {
      const fragment = core.parseFragment(text);
      keep(fragment.s);
      return fragment;
    },
    deriveLink: async (id: Uint8Array, s: Uint8Array) => {
      const keys = await core.deriveLink(id, s);
      keep(keys.r);
      keep(keys.saltPw);
      return keys;
    },
    deriveContent: async (id: Uint8Array, s: Uint8Array, p: Uint8Array) => {
      const keys = await core.deriveContent(id, s, p);
      keep(keys.v);
      return keys;
    },
  };
});

const ORIGIN = 'https://nylzo.test';

function zeroed(): boolean[] {
  return captured.map((bytes) => bytes.length > 0 && bytes.every((byte) => byte === 0));
}

async function created(api: FakeApi, opensMax = 1): Promise<string> {
  const deps: CreateDeps = { api, origin: ORIGIN, scrypt: async () => new Uint8Array(32), solvePow: async () => '' };
  const message = await createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax, passphrase: '', kdfId: 0 }, deps);
  captured.length = 0;
  return message.link;
}

describe('secrets are wiped after use (SEC-8, D39, D41)', () => {
  beforeEach(() => {
    captured.length = 0;
  });

  it('wipes S, R, salt_pw and V after creating a message', async () => {
    const api = fakeApi();
    const deps: CreateDeps = { api, origin: ORIGIN, scrypt: async () => new Uint8Array(32), solvePow: async () => '' };
    await createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '', kdfId: 0 }, deps);
    expect(captured).toHaveLength(4);
    expect(zeroed()).toEqual([true, true, true, true]);
  });

  it('wipes S, R and salt_pw after reporting a pasted link', async () => {
    const api = fakeApi();
    const link = await created(api);
    expect(await reportLink(api, ORIGIN, link, 'spam')).toEqual({ outcome: 'accepted' });
    expect(captured).toHaveLength(3);
    expect(zeroed()).toEqual([true, true, true]);
  });

  it('wipes S when it refuses a link of another service', async () => {
    const api = fakeApi();
    const link = await created(api);
    expect(await reportLink(api, 'https://other.test', link, '')).toEqual({ outcome: 'invalid', problems: ['link_foreign'] });
    expect(captured).toHaveLength(1);
    expect(zeroed()).toEqual([true]);
  });

  it('wipes S, R, salt_pw and V once the reader shows the message', async () => {
    const api = fakeApi();
    const url = new URL(await created(api, 2));
    const reader = createReader(
      {
        api,
        location: { pathname: url.pathname, hash: url.hash },
        session: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined },
        page: { visible: () => true, prerendering: () => false },
        dropFragment: () => undefined,
        scrypt: async () => new Uint8Array(32),
        now: () => Date.now(),
      },
      () => undefined,
    );
    await reader.load();
    await reader.reveal();
    expect(captured).toHaveLength(4);
    expect(zeroed()).toEqual([true, true, true, true]);
    expect(await reader.report('after reading')).toEqual({ outcome: 'accepted' });
  });
});
