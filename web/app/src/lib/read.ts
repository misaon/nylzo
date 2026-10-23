import {
  KDF_NONE,
  KDF_PBKDF2,
  deriveContent,
  deriveLink,
  derivePassphraseKey,
  idToString,
  open,
  parseFragment,
  parsePath,
  requestKey,
  type KdfId,
  type RequestKey,
} from '@nylzo/core';

import { ApiError, type Api, type Envelope, type MessageState, type Metadata } from './api.ts';

export type EndedState = Exclude<MessageState, 'available'>;

export type BusyStep = 'deriving' | 'claiming' | 'downloading' | 'decrypting';

export interface RevealedFile {
  readonly name: string;
  readonly mime: string;
  readonly bytes: Uint8Array<ArrayBuffer>;
}

export type ReadView =
  | { readonly view: 'loading' }
  | { readonly view: 'invalid' }
  | { readonly view: 'unknown' }
  | { readonly view: 'tampered' }
  | { readonly view: 'failed' }
  | { readonly view: 'retry_later'; readonly retryAfter?: number | undefined }
  | {
      readonly view: 'ended';
      readonly state: EndedState;
      readonly firstOpenedAt?: string | undefined;
      readonly endedAt?: string | undefined;
    }
  | {
      readonly view: 'ready';
      readonly metadata: Metadata;
      readonly resumable: boolean;
      readonly deviceFailed: boolean;
      readonly rejected?: PassphraseRejection | undefined;
    }
  | { readonly view: 'busy'; readonly step: BusyStep; readonly fraction?: number | undefined }
  | { readonly view: 'revealed'; readonly text?: string | undefined; readonly file?: RevealedFile | undefined };

export type PassphraseRejection =
  | { readonly reason: 'malformed' }
  | { readonly reason: 'wrong'; readonly attemptsLeft: number; readonly retryAfter: number; readonly locked: boolean };

export interface SessionStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface ReaderDeps {
  readonly api: Api;
  readonly location: { readonly pathname: string; readonly hash: string };
  readonly session: SessionStore;
  readonly page: { visible(): boolean; prerendering(): boolean };
  readonly dropFragment: () => void;
  readonly scrypt: (passphrase: string, saltPw: Uint8Array<ArrayBuffer>, onProgress: (fraction: number) => void) => Promise<Uint8Array<ArrayBuffer>>;
  readonly now: () => number;
}

export interface Reader {
  load(): Promise<void>;
  reveal(passphrase?: string): Promise<void>;
}

function isTerminal(state: MessageState): state is Exclude<EndedState, 'opened'> {
  return state === 'expired' || state === 'cancelled' || state === 'deleted';
}

function safely<T>(action: () => T, fallback: T): T {
  try {
    return action();
  } catch {
    return fallback;
  }
}

export function createReader(deps: ReaderDeps, notify: (view: ReadView) => void): Reader {
  let link: { id: Uint8Array<ArrayBuffer>; kdfId: KdfId; s: Uint8Array<ArrayBuffer> } | undefined;
  let readKey: RequestKey | undefined;
  let saltPw: Uint8Array<ArrayBuffer> | undefined;
  let metadata: Metadata | undefined;
  let deviceFailed = false;
  let busy = false;
  let lease: string | undefined;
  let revealable = false;

  const onView = (view: ReadView) => {
    revealable = view.view === 'ready';
    notify(view);
  };

  const leaseKey = () => `nylzo:lease:${idToString(link?.id ?? new Uint8Array(16))}`;
  const kdfKey = () => `nylzo:kdf:${idToString(link?.id ?? new Uint8Array(16))}`;
  const storedLease = () => lease ?? safely(() => deps.session.getItem(leaseKey()), null);
  const keepLease = (value: string) => {
    lease = value;
    safely(() => deps.session.setItem(leaseKey(), value), undefined);
  };
  const forgetLease = () => {
    lease = undefined;
    safely(() => deps.session.removeItem(leaseKey()), undefined);
  };

  function ended(state: EndedState, data: Pick<Metadata, 'firstOpenedAt' | 'endedAt'> = {}): ReadView {
    forgetLease();
    return { view: 'ended', state, firstOpenedAt: data.firstOpenedAt, endedAt: data.endedAt };
  }

  function apiView(error: unknown): ReadView {
    if (!(error instanceof ApiError)) {
      return { view: 'failed' };
    }
    switch (error.kind) {
      case 'not_found':
        return { view: 'unknown' };
      case 'gone':
        return error.state && error.state !== 'available' ? ended(error.state) : { view: 'failed' };
      case 'rate_limited':
      case 'unavailable':
        return { view: 'retry_later', retryAfter: error.retryAfter };
      default:
        return { view: 'failed' };
    }
  }

  function readyView(rejected?: PassphraseRejection): ReadView {
    if (!metadata) {
      return { view: 'failed' };
    }
    return { view: 'ready', metadata, resumable: storedLease() !== null, deviceFailed, rejected };
  }

  async function load(): Promise<void> {
    onView({ view: 'loading' });
    try {
      const id = parsePath(deps.location.pathname);
      const fragment = parseFragment(deps.location.hash);
      link = { id, kdfId: fragment.kdfId, s: fragment.s };
    } catch {
      onView({ view: 'invalid' });
      return;
    }
    if (safely(() => deps.session.getItem(kdfKey()), null) !== null) {
      deviceFailed = true;
      safely(() => deps.session.removeItem(kdfKey()), undefined);
    }
    try {
      const keys = await deriveLink(link.id, link.s);
      saltPw = keys.saltPw;
      readKey = await requestKey(keys.r);
      metadata = await deps.api.metadata(link.id, readKey);
    } catch (error) {
      onView(apiView(error));
      return;
    }
    if (isTerminal(metadata.state)) {
      onView(ended(metadata.state, metadata));
      return;
    }
    if (metadata.kdfId !== link.kdfId || metadata.passphraseRequired !== (link.kdfId !== KDF_NONE)) {
      onView({ view: 'tampered' });
      return;
    }
    if (metadata.state === 'opened' && storedLease() === null) {
      onView(ended('opened', metadata));
      return;
    }
    onView(readyView());
  }

  async function passphraseKey(passphrase: string): Promise<Uint8Array<ArrayBuffer>> {
    if (!link || !saltPw) {
      throw new Error('reader not loaded');
    }
    if (link.kdfId === KDF_NONE) {
      return new Uint8Array(0);
    }
    onView({ view: 'busy', step: 'deriving', fraction: 0 });
    if (link.kdfId === KDF_PBKDF2) {
      return derivePassphraseKey(KDF_PBKDF2, passphrase, saltPw);
    }
    safely(() => deps.session.setItem(kdfKey(), '1'), undefined);
    try {
      return await deps.scrypt(passphrase, saltPw, (fraction) => onView({ view: 'busy', step: 'deriving', fraction }));
    } finally {
      safely(() => deps.session.removeItem(kdfKey()), undefined);
    }
  }

  function rejection(error: ApiError): PassphraseRejection {
    const retryAfter = error.retryAfter ?? 0;
    const expiresAt = metadata?.expiresAt ? Date.parse(metadata.expiresAt) : Number.NaN;
    const locked = (error.attemptsLeft ?? 0) === 0 && Number.isFinite(expiresAt) && deps.now() + retryAfter * 1000 >= expiresAt;
    return { reason: 'wrong', attemptsLeft: error.attemptsLeft ?? 0, retryAfter, locked };
  }

  async function fetchEnvelope(id: Uint8Array, key: RequestKey, verifierKey: RequestKey | undefined): Promise<Envelope | ReadView> {
    const onProgress = (received: number, total: number | undefined) =>
      onView({ view: 'busy', step: 'downloading', fraction: total ? received / total : undefined });
    const previous = storedLease();
    if (previous !== null) {
      try {
        const envelope = await deps.api.blob(id, key, previous, onProgress);
        lease = previous;
        return envelope;
      } catch (error) {
        if (!(error instanceof ApiError) || error.kind !== 'gone') {
          return apiView(error);
        }
        forgetLease();
        if (error.state !== 'available') {
          return apiView(error);
        }
      }
    }
    onView({ view: 'busy', step: 'claiming' });
    try {
      return await deps.api.claim(id, key, {
        verifierKey,
        onLease: keepLease,
        onProgress,
      });
    } catch (error) {
      if (error instanceof ApiError && error.kind === 'passphrase') {
        return readyView(rejection(error));
      }
      return apiView(error);
    }
  }

  async function revealOnce(passphrase: string): Promise<ReadView> {
    if (!link || !readKey || !metadata) {
      return { view: 'failed' };
    }
    let content;
    try {
      content = await deriveContent(link.id, link.s, await passphraseKey(passphrase));
    } catch (error) {
      if (error instanceof Error && error.name === 'MalformedPassphraseError') {
        return readyView({ reason: 'malformed' });
      }
      if (error instanceof Error && error.name === 'KdfWorkerError') {
        deviceFailed = true;
        return readyView();
      }
      return { view: 'failed' };
    }
    const verifierKey = link.kdfId === KDF_NONE ? undefined : await requestKey(content.v);
    const envelope = await fetchEnvelope(link.id, readKey, verifierKey);
    if ('view' in envelope) {
      return envelope;
    }
    if (envelope.policy.kdfId !== link.kdfId || envelope.policy.flags !== 0) {
      return { view: 'tampered' };
    }
    onView({ view: 'busy', step: 'decrypting' });
    let opened;
    try {
      opened = await open(
        content.k,
        link.id,
        { opensMax: envelope.policy.opensMax, ttlSeconds: envelope.policy.ttlSeconds },
        link.kdfId,
        envelope.object,
      );
    } catch {
      return { view: 'tampered' };
    }
    const acknowledged = storedLease();
    if (acknowledged !== null) {
      await deps.api.ack(link.id, readKey, acknowledged).catch(() => undefined);
    }
    forgetLease();
    deps.dropFragment();
    const { text, file } = opened.payload.meta;
    return {
      view: 'revealed',
      text,
      file: file && opened.payload.file ? { name: file.name, mime: file.mime, bytes: new Uint8Array(opened.payload.file) } : undefined,
    };
  }

  async function reveal(passphrase = ''): Promise<void> {
    if (!revealable || busy || !deps.page.visible() || deps.page.prerendering()) {
      return;
    }
    busy = true;
    try {
      onView(await revealOnce(passphrase));
    } finally {
      busy = false;
    }
  }

  return { load, reveal };
}

