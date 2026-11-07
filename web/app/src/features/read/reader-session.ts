import { servedByLocalClient } from '../../shared/local-client/instance-origin.ts';
import type { SessionStore } from './reader.ts';

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
