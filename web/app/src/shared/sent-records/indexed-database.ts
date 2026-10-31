const DATABASE_VERSION = 1;
const OPEN_TIMEOUT_MS = 2000;

class StorageUnavailableError extends Error {
  override readonly name = 'StorageUnavailableError';

  constructor() {
    super('this browser did not let the page store data');
  }
}

export function browserDatabases(): IDBFactory | undefined {
  return typeof indexedDB === 'undefined' ? undefined : indexedDB;
}

export async function databaseExists(factory: IDBFactory | undefined, name: string): Promise<boolean> {
  const databases = (await factory?.databases()) ?? [];
  return databases.some((database) => database.name === name);
}

export async function openDatabase(
  factory: IDBFactory | undefined,
  name: string,
  createStores: (database: IDBDatabase) => void,
): Promise<IDBDatabase> {
  if (!factory) {
    throw new StorageUnavailableError();
  }
  const request = factory.open(name, DATABASE_VERSION);
  const { promise, resolve, reject } = Promise.withResolvers<IDBDatabase>();
  let abandoned = false;
  const abandon = (error: unknown) => {
    abandoned = true;
    clearTimeout(timeout);
    reject(error);
  };
  const timeout = setTimeout(() => abandon(new StorageUnavailableError()), OPEN_TIMEOUT_MS);
  request.addEventListener('upgradeneeded', () => createStores(request.result));
  request.addEventListener('success', () => {
    clearTimeout(timeout);
    if (abandoned) {
      request.result.close();
    } else {
      resolve(request.result);
    }
  });
  request.addEventListener('error', () => abandon(request.error ?? new StorageUnavailableError()));
  request.addEventListener('blocked', () => abandon(new StorageUnavailableError()));
  return promise;
}

export async function requestResult<Result>(request: IDBRequest<Result>): Promise<Result> {
  return new Promise((resolve, reject) => {
    request.addEventListener('success', () => resolve(request.result));
    request.addEventListener('error', () => reject(request.error ?? new StorageUnavailableError()));
  });
}

export async function committed(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    const fail = () => reject(transaction.error ?? new StorageUnavailableError());
    transaction.addEventListener('complete', () => resolve());
    transaction.addEventListener('error', fail);
    transaction.addEventListener('abort', fail);
  });
}
