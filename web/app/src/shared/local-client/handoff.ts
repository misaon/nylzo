import { readJson } from '../api/responses.ts';
import { servedByLocalClient } from './instance-origin.ts';

export const HANDOFF_PATH = '/open';

const PATH = /^\/r$/u;
const FRAGMENT = /^[\w.-]+$/u;
const TOKEN = /^#[A-Z2-7]{26}$/u;
const HANDOFF_TIMEOUT_MS = 5000;

const handedOver: { fragment: string | undefined } = { fragment: undefined };

function isHandoff(value: unknown): value is { readonly path: string; readonly fragment: string } {
  if (typeof value !== 'object' || value === null) {
    return false;
  }
  const path: unknown = Reflect.get(value, 'path');
  const fragment: unknown = Reflect.get(value, 'fragment');
  return typeof path === 'string' && typeof fragment === 'string' && PATH.test(path) && FRAGMENT.test(fragment);
}

async function redeem(token: string): Promise<unknown> {
  const response = await fetch(HANDOFF_PATH, {
    method: 'POST',
    body: token,
    cache: 'no-store',
    credentials: 'omit',
    redirect: 'error',
    referrerPolicy: 'no-referrer',
    signal: AbortSignal.timeout(HANDOFF_TIMEOUT_MS),
  });
  return response.ok ? readJson(response) : undefined;
}

// The link comes from `nylzo open` in memory, so no navigation ever puts its key into the browser history.
export async function takeHandoff(): Promise<void> {
  if (location.pathname !== HANDOFF_PATH) {
    return;
  }
  const token = globalThis.nylzoFragment?.() ?? location.hash;
  history.replaceState(null, '', HANDOFF_PATH);
  // Only the local client redeems a token; anywhere else the text after # may be a link key.
  if (!servedByLocalClient() || !TOKEN.test(token)) {
    return;
  }
  try {
    const body = await redeem(token.slice(1));
    if (isHandoff(body)) {
      history.replaceState(null, '', body.path);
      handedOver.fragment = `#${body.fragment}`;
    }
  } catch {}
}

export function takeHandedOverFragment(): string | undefined {
  const { fragment } = handedOver;
  handedOver.fragment = undefined;
  return fragment;
}
