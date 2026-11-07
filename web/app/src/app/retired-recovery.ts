const RETIRED_DATABASE = 'nylzo-resume';
// Older clients kept link pointers and named each message in their scrypt mark.
const RETIRED_KEYS = ['nylzo:link:', 'nylzo:kdf:'];

function forgetPointers(session: () => Storage): void {
  try {
    const storage = session();
    for (let index = storage.length - 1; index >= 0; index--) {
      const name = storage.key(index);
      if (name !== null && RETIRED_KEYS.some((prefix) => name.startsWith(prefix))) {
        storage.removeItem(name);
      }
    }
  } catch {}
}

export function forgetRetiredRecovery(session: () => Storage, databases: IDBFactory | undefined): void {
  forgetPointers(session);
  try {
    databases?.deleteDatabase(RETIRED_DATABASE);
  } catch {}
}
