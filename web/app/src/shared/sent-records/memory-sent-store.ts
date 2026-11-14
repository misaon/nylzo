import { storedForm } from './sent-record.ts';
import type { SentRecord } from './sent-record.ts';
import type { SentStore } from './sent-store.ts';

export function memoryStore(
  initial: readonly SentRecord[] = [],
  broken = false,
): SentStore & { readonly records: Map<string, SentRecord> } {
  const records = new Map(initial.map((record) => [record.id, record]));
  const check = () => {
    if (broken) {
      throw new Error('storage disabled');
    }
  };
  /* oxlint-disable typescript/require-await -- the methods stay async like SentStore, so a refused write rejects the promise as with IndexedDB */
  return {
    records,
    async list() {
      check();
      return Array.from(records.values(), (record) => ({ ...record }));
    },
    async put(record) {
      check();
      const { id, createdAt, expiresAt, label } = storedForm(record);
      records.set(id, { id, createdAt, expiresAt, ...(label !== undefined && { label }), manageKey: record.manageKey });
    },
    async remove(id) {
      check();
      records.delete(id);
    },
    async clear() {
      check();
      records.clear();
    },
  };
  /* oxlint-enable typescript/require-await -- only the store methods above */
}
