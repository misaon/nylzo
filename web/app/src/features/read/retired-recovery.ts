const RETIRED_DATABASE = 'nylzo-resume';
const RETIRED_POINTER = 'nylzo:link:';

function forgetPointers(session: () => Storage): void {
  try {
    const storage = session();
    for (let index = storage.length - 1; index >= 0; index--) {
      const name = storage.key(index);
      if (name?.startsWith(RETIRED_POINTER)) {
        storage.removeItem(name);
      }
    }
  } catch {
    return;
  }
}

export function forgetRetiredRecovery(session: () => Storage, databases: IDBFactory | undefined): void {
  forgetPointers(session);
  try {
    databases?.deleteDatabase(RETIRED_DATABASE);
  } catch {
    return;
  }
}
