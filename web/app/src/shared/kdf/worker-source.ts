import kdfWorkerUrl from './kdf-worker.ts?worker&url';

class WorkerSourceError extends Error {
  override readonly name = 'WorkerSourceError';

  constructor(message = 'the key derivation worker failed its integrity check or did not load') {
    super(message);
  }
}

const createdUrls = new Set<string>();

const workerPolicy =
  typeof trustedTypes === 'undefined'
    ? undefined
    : trustedTypes.createPolicy('nylzo-kdf', {
        createScriptURL(input) {
          if (!createdUrls.has(input)) {
            throw new TypeError('unexpected worker url');
          }
          return input;
        },
      });

const verified: { source?: Promise<Blob> } = {};

function importMapIntegrity(url: string): string | undefined {
  const map = document.querySelector('script[type="importmap"]')?.textContent;
  if (map === undefined) {
    return;
  }
  const parsed: unknown = JSON.parse(map);
  const integrity: unknown =
    typeof parsed === 'object' && parsed !== null ? Reflect.get(parsed, 'integrity') : undefined;
  const value: unknown = typeof integrity === 'object' && integrity !== null ? Reflect.get(integrity, url) : undefined;
  return typeof value === 'string' ? value : undefined;
}

async function fetchVerified(): Promise<Blob> {
  const integrity = importMapIntegrity(kdfWorkerUrl);
  if (integrity === undefined && !import.meta.env.DEV) {
    throw new WorkerSourceError('the page names no hash for the key derivation worker');
  }
  const response = await fetch(kdfWorkerUrl, {
    ...(integrity !== undefined && { integrity }),
    cache: 'no-store',
    credentials: 'omit',
    redirect: 'error',
    referrerPolicy: 'no-referrer',
  });
  if (!response.ok) {
    throw new WorkerSourceError();
  }
  return new Blob([await response.arrayBuffer()], { type: 'text/javascript' });
}

async function loadWorkerSource(): Promise<Blob> {
  verified.source ??= fetchVerified();
  try {
    return await verified.source;
  } catch (error) {
    delete verified.source;
    throw error;
  }
}

export async function prepareWorker(): Promise<void> {
  try {
    await loadWorkerSource();
  } catch {
    return;
  }
}

export interface StartedWorker {
  readonly worker: Worker;
  readonly stop: () => void;
}

export async function startWorker(): Promise<StartedWorker> {
  let blob: Blob;
  try {
    blob = await loadWorkerSource();
  } catch {
    throw new WorkerSourceError();
  }
  const url = URL.createObjectURL(blob);
  createdUrls.add(url);
  const stop = () => URL.revokeObjectURL(url);
  try {
    // Worker accepts a TrustedScriptURL; the DOM typings only know strings.
    const scriptUrl = (workerPolicy?.createScriptURL(url) ?? url) as unknown as string;
    const worker = new Worker(scriptUrl, { type: 'module', name: 'nylzo-kdf' });
    return {
      worker,
      stop: () => {
        worker.terminate();
        stop();
      },
    };
  } catch (error) {
    stop();
    throw error;
  } finally {
    createdUrls.delete(url);
  }
}
