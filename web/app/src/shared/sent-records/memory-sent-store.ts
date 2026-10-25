import type { SentStore } from './sent-store.ts';

import { type SentRecord, storedForm } from './sent-record.ts';

export function memoryStore(
  initial: SentRecord[] = [],
  broken = false,
): SentStore & { readonly records: Map<string, SentRecord> } {
  const records = new Map(initial.map((record) => [record.id, record]));
  const check = () => {
    if (broken) {
      throw new Error('storage disabled');
    }
  };
  /* eslint-disable @typescript-eslint/require-await -- the fake keeps the async methods of SentStore, so a refused write rejects the promise as with IndexedDB */
  return {
    records,
    async list() {
      check();
      return Array.from(records.values(), (record) => ({ ...record }));
    },
    async put(record) {
      check();
      storedForm(record);
      records.set(record.id, record);
    },
    async markEnded(id, endedAt) {
      check();
      const record = records.get(id);
      if (!record || record.endedAt !== undefined) {
        return record;
      }
      const ended = { ...record, endedAt };
      records.set(id, ended);
      return ended;
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
  /* eslint-enable @typescript-eslint/require-await -- only the fake methods above */
}
