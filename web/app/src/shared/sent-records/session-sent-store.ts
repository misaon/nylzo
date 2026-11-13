import { createContext } from 'svelte';

import { memoryStore } from './memory-sent-store.ts';
import type { SentStore } from './sent-store.ts';

export interface SessionSentStore extends SentStore {
  readonly held: () => boolean;
}

export function sessionSentStore(): SessionSentStore {
  const store = memoryStore();
  return { ...store, held: () => store.records.size > 0 };
}

export const [getSessionSentStore, setSessionSentStore] = createContext<SessionSentStore>();
