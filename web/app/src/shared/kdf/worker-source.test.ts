import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import kdfWorkerUrl from './kdf-worker.ts?worker&url';

describe('verified worker source', () => {
  const fetchSource = vi.fn<typeof fetch>();
  const terminate = vi.fn();
  const createObjectURL = vi.fn<() => string>();
  const revokeObjectURL = vi.fn();
  const construct = vi.fn();

  beforeEach(() => {
    vi.resetModules();
    vi.resetAllMocks();
    fetchSource.mockResolvedValue(new Response('/* synthetic worker */'));
    createObjectURL.mockReturnValue('blob:synthetic-worker');
    vi.stubGlobal('fetch', fetchSource);
    vi.stubGlobal('document', {
      querySelector: () => ({ textContent: JSON.stringify({ integrity: { [kdfWorkerUrl]: 'sha384-synthetic' } }) }),
    });
    vi.stubGlobal(
      'Worker',
      class {
        terminate = terminate;
        constructor(url: string, options: WorkerOptions) {
          construct(url, options);
        }
      },
    );
    vi.spyOn(URL, 'createObjectURL').mockImplementation(createObjectURL);
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(revokeObjectURL);
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it('prefetches once, reuses verified bytes, and revokes each worker URL when stopped', async () => {
    const { prepareWorker, startWorker } = await import('./worker-source.ts');
    await Promise.all([prepareWorker(), prepareWorker()]);
    const first = await startWorker();
    const second = await startWorker();
    expect(fetchSource).toHaveBeenCalledExactlyOnceWith(
      expect.any(String),
      expect.objectContaining({
        cache: 'no-store',
        credentials: 'omit',
        redirect: 'error',
        referrerPolicy: 'no-referrer',
        integrity: 'sha384-synthetic',
      }),
    );
    expect(construct).toHaveBeenCalledWith('blob:synthetic-worker', { type: 'module', name: 'nylzo-kdf' });
    first.stop();
    second.stop();
    expect(terminate).toHaveBeenCalledTimes(2);
    expect(revokeObjectURL).toHaveBeenCalledTimes(2);
  });

  it('does not poison the cache when fetching fails, and retries on the next start', async () => {
    fetchSource.mockResolvedValueOnce(new Response('', { status: 503 }));
    const { prepareWorker, startWorker } = await import('./worker-source.ts');
    await prepareWorker();
    const started = await startWorker();
    expect(fetchSource).toHaveBeenCalledTimes(2);
    started.stop();
  });

  it('refuses worker creation when prefetch and startup cannot verify the source', async () => {
    fetchSource.mockRejectedValue(new Error('integrity mismatch'));
    const { startWorker } = await import('./worker-source.ts');
    await expect(startWorker()).rejects.toMatchObject({ name: 'WorkerSourceError' });
    expect(construct).not.toHaveBeenCalled();
    expect(createObjectURL).not.toHaveBeenCalled();
  });

  it('requires an integrity entry outside development mode before fetching any code', async () => {
    vi.stubEnv('DEV', false);
    vi.stubGlobal('document', { querySelector: () => null });
    const { startWorker } = await import('./worker-source.ts');
    await expect(startWorker()).rejects.toMatchObject({ name: 'WorkerSourceError' });
    expect(fetchSource).not.toHaveBeenCalled();
  });

  it('revokes the object URL if constructing the worker fails', async () => {
    construct.mockImplementation(() => {
      throw new Error('CSP refused');
    });
    const { startWorker } = await import('./worker-source.ts');
    await expect(startWorker()).rejects.toThrow('CSP refused');
    expect(revokeObjectURL).toHaveBeenCalledExactlyOnceWith('blob:synthetic-worker');
  });

  it('restricts the Trusted Types policy to URLs being constructed at that moment', async () => {
    let policy: { createScriptURL?: (input: string) => string } | undefined;
    vi.stubGlobal('trustedTypes', {
      createPolicy: (_name: string, options: { createScriptURL?: (input: string) => string }) => {
        policy = options;
        return options;
      },
    });
    construct.mockImplementation((url: string) => {
      expect(policy?.createScriptURL?.(url)).toBe(url);
    });
    const { startWorker } = await import('./worker-source.ts');
    const started = await startWorker();
    expect(() => policy?.createScriptURL?.('https://untrusted.test/worker.js')).toThrow('unexpected worker url');
    expect(() => policy?.createScriptURL?.('blob:synthetic-worker')).toThrow('unexpected worker url');
    started.stop();
  });
});
