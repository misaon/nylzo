export const HANDOFF_PATH = '/open';

const PATH = /^\/[\w/-]*$/;
const FRAGMENT = /^[\w.-]+$/;

let handedOver: string | undefined;

function isHandoff(value: unknown): value is { readonly path: string; readonly fragment: string } {
  if (typeof value !== 'object' || value === null) {
    return false;
  }
  const { path, fragment } = value as Record<string, unknown>;
  return typeof path === 'string' && typeof fragment === 'string' && PATH.test(path) && FRAGMENT.test(fragment);
}

// The link comes from `nylzo open` in memory, so no navigation ever puts its key into the browser history.
export async function takeHandoff(): Promise<void> {
  if (location.pathname !== HANDOFF_PATH) {
    return;
  }
  const token = location.hash.slice(1);
  history.replaceState(null, '', HANDOFF_PATH);
  if (token === '') {
    return;
  }
  try {
    const response = await fetch(HANDOFF_PATH, { method: 'POST', body: token, cache: 'no-store' });
    const body: unknown = response.ok ? await response.json() : undefined;
    if (isHandoff(body)) {
      history.replaceState(null, '', body.path);
      handedOver = `#${body.fragment}`;
    }
  } catch {
    return;
  }
}

export function takeHandedOverFragment(): string | undefined {
  const fragment = handedOver;
  handedOver = undefined;
  return fragment;
}
