import { encodeBase64url, utf8 } from '@nylzo/core';

import { servedByLocalClient } from '../../shared/local-client/instance-origin.ts';
import type { SessionStore } from './reader.ts';

const LEASE_TAG_LABEL = utf8('nylzo/v3/lease-tag');
const LEASE_TAG_BYTES = 16;

// Names a stored lease without the message id; only the key in the link can compute it again.
export async function leaseTag(secret: Uint8Array): Promise<string> {
  const input = new Uint8Array(LEASE_TAG_LABEL.length + secret.length);
  input.set(LEASE_TAG_LABEL);
  input.set(secret, LEASE_TAG_LABEL.length);
  try {
    const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', input));
    return encodeBase64url(digest.subarray(0, LEASE_TAG_BYTES));
  } finally {
    input.fill(0);
  }
}

// A session of the local client has an origin nobody purges later, so its lease lives only in memory (Protocol §14.1).
export function readerSession(): SessionStore {
  if (servedByLocalClient()) {
    const values = new Map<string, string>();
    return {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => {
        values.set(key, value);
      },
      removeItem: (key) => {
        values.delete(key);
      },
    };
  }
  return {
    getItem: (key) => sessionStorage.getItem(key),
    setItem: (key, value) => sessionStorage.setItem(key, value),
    removeItem: (key) => sessionStorage.removeItem(key),
  };
}
