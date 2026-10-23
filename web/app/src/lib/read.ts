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
import { sanitizeFileName } from './download.ts';
import { sendReport, type ReportOutcome } from './report.ts';

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
  | { readonly view: 'revealed'; readonly text?: string | undefined; readonly file?: RevealedFile | undefined; readonly burnable: boolean };

export type BurnOutcome =
  | { readonly outcome: 'deleted' }
  | { readonly outcome: 'ended'; readonly state: EndedState }
  | { readonly outcome: 'unknown' }
  | { readonly outcome: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly outcome: 'failed' }
  | { readonly outcome: 'ignored' };

export type PassphraseRejection =
  | { readonly reason: 'malformed' }
  | { readonly reason: 'wrong_or_changed' }
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
  burn(): Promise<BurnOutcome>;
  report(reason: string): Promise<ReportOutcome>;
}

interface Secrets {
  readonly id: Uint8Array<ArrayBuffer>;
  readonly kdfId: KdfId;
  readonly s: Uint8Array<ArrayBuffer>;
  readonly saltPw: Uint8Array<ArrayBuffer>;
  readonly readKey: RequestKey;
}

interface Held {
  readonly id: Uint8Array<ArrayBuffer>;
  readonly readKey: RequestKey;
}

type Revealed = Extract<ReadView, { view: 'revealed' }>;

interface Fetched {
  readonly envelope: Envelope;
  readonly lease: string;
  readonly fromLease: boolean;
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
  let secrets: Secrets | undefined;
  let held: Held | undefined;
  let revealed: Revealed | undefined;
  let reporting = false;
  let idText = '';
  let metadata: Metadata | undefined;
  let deviceFailed = false;
  let busy = false;
  let lease: string | undefined;
  let revealable = false;

  const onView = (view: ReadView) => {
    revealable = view.view === 'ready';
    revealed = view.view === 'revealed' ? view : undefined;
    if (view.view !== 'ready' && view.view !== 'busy') {
      forget();
    }
    if (view.view !== 'ready' && view.view !== 'busy' && view.view !== 'revealed') {
      held = undefined;
    }
    notify(view);
  };

  const leaseKey = () => `nylzo:lease:${idText}`;
  const kdfKey = () => `nylzo:kdf:${idText}`;
  const storedLease = () => lease ?? safely(() => deps.session.getItem(leaseKey()), null);
  const keepLease = (value: string) => {
    lease = value;
    safely(() => deps.session.setItem(leaseKey(), value), undefined);
  };
  const forgetLease = () => {
    lease = undefined;
    safely(() => deps.session.removeItem(leaseKey()), undefined);
  };

  function forget(): void {
    secrets?.s.fill(0);
    secrets?.saltPw.fill(0);
    secrets = undefined;
  }

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
    if (!metadata || !secrets) {
      return { view: 'failed' };
    }
    return { view: 'ready', metadata, resumable: storedLease() !== null, deviceFailed, rejected };
  }

  function metadataView(current: Metadata, kdfId: KdfId): ReadView {
    if (current.endedAt !== undefined) {
      return current.state === 'available' ? { view: 'failed' } : ended(current.state, current);
    }
    if (isTerminal(current.state)) {
      return ended(current.state, current);
    }
    if (current.kdfId !== kdfId || current.passphraseRequired !== (kdfId !== KDF_NONE)) {
      return { view: 'tampered' };
    }
    if (current.state === 'opened' && storedLease() === null) {
      return ended('opened', current);
    }
    return readyView();
  }

  async function load(): Promise<void> {
    onView({ view: 'loading' });
    let fragment;
    try {
      const id = parsePath(deps.location.pathname);
      fragment = { id, ...parseFragment(deps.location.hash) };
    } catch {
      onView({ view: 'invalid' });
      return;
    }
    idText = idToString(fragment.id);
    if (safely(() => deps.session.getItem(kdfKey()), null) !== null) {
      deviceFailed = true;
      safely(() => deps.session.removeItem(kdfKey()), undefined);
    }
    try {
      const keys = await deriveLink(fragment.id, fragment.s);
      const readKey = await requestKey(keys.r);
      keys.r.fill(0);
      secrets = { id: fragment.id, kdfId: fragment.kdfId, s: fragment.s, saltPw: keys.saltPw, readKey };
      held = { id: fragment.id, readKey };
      metadata = await deps.api.metadata(fragment.id, readKey);
    } catch (error) {
      onView(apiView(error));
      return;
    }
    onView(metadataView(metadata, fragment.kdfId));
  }

  async function passphraseKey(current: Secrets, passphrase: string): Promise<Uint8Array<ArrayBuffer>> {
    if (current.kdfId === KDF_NONE) {
      return new Uint8Array(0);
    }
    if (current.kdfId === KDF_PBKDF2) {
      onView({ view: 'busy', step: 'deriving' });
      return derivePassphraseKey(KDF_PBKDF2, passphrase, current.saltPw);
    }
    onView({ view: 'busy', step: 'deriving', fraction: 0 });
    safely(() => deps.session.setItem(kdfKey(), '1'), undefined);
    try {
      return await deps.scrypt(passphrase, current.saltPw, (fraction) => onView({ view: 'busy', step: 'deriving', fraction }));
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

  async function fetchEnvelope(current: Secrets, verifierKey: RequestKey | undefined): Promise<Fetched | ReadView> {
    const onProgress = (received: number, total: number | undefined) =>
      onView({ view: 'busy', step: 'downloading', fraction: total ? received / total : undefined });
    const previous = storedLease();
    if (previous !== null) {
      try {
        const envelope = await deps.api.blob(current.id, current.readKey, previous, onProgress);
        lease = previous;
        return { envelope, lease: previous, fromLease: true };
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
      const claimed = await deps.api.claim(current.id, current.readKey, { verifierKey, onLease: keepLease, onProgress });
      return { envelope: claimed, lease: claimed.lease, fromLease: false };
    } catch (error) {
      if (error instanceof ApiError && error.kind === 'passphrase') {
        return readyView(rejection(error));
      }
      return apiView(error);
    }
  }

  function policyMatches(envelope: Envelope, current: Secrets): boolean {
    const served = envelope.policy;
    return (
      served.flags === 0 &&
      served.kdfId === current.kdfId &&
      metadata !== undefined &&
      served.opensMax === metadata.opensMax &&
      served.ttlSeconds === metadata.ttlSeconds
    );
  }

  async function revealOnce(current: Secrets, passphrase: string): Promise<ReadView | undefined> {
    let content;
    try {
      const p = await passphraseKey(current, passphrase);
      content = await deriveContent(current.id, current.s, p);
      p.fill(0);
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
    const verifierKey = current.kdfId === KDF_NONE ? undefined : await requestKey(content.v);
    content.v.fill(0);
    const fetched = await fetchEnvelope(current, verifierKey);
    if ('view' in fetched) {
      return fetched;
    }
    if (!policyMatches(fetched.envelope, current)) {
      return { view: 'tampered' };
    }
    onView({ view: 'busy', step: 'decrypting' });
    let opened;
    try {
      const policy = { opensMax: fetched.envelope.policy.opensMax, ttlSeconds: fetched.envelope.policy.ttlSeconds };
      opened = await open(content.k, current.id, policy, current.kdfId, fetched.envelope.object);
    } catch {
      return fetched.fromLease && current.kdfId !== KDF_NONE ? readyView({ reason: 'wrong_or_changed' }) : { view: 'tampered' };
    }
    deps.dropFragment();
    const { text, file } = opened.payload.meta;
    const remaining = (metadata?.opensRemaining ?? 0) - (fetched.fromLease ? 0 : 1);
    onView({
      view: 'revealed',
      text,
      file: file && opened.payload.file ? { name: sanitizeFileName(file.name), mime: file.mime, bytes: new Uint8Array(opened.payload.file) } : undefined,
      burnable: remaining > 0,
    });
    await deps.api.ack(current.id, current.readKey, fetched.lease).catch(() => undefined);
    forgetLease();
    return undefined;
  }

  async function reveal(passphrase = ''): Promise<void> {
    const current = secrets;
    if (!current || !revealable || busy || !deps.page.visible() || deps.page.prerendering()) {
      return;
    }
    busy = true;
    try {
      const view = await revealOnce(current, passphrase);
      if (view) {
        onView(view);
      }
    } catch {
      onView({ view: 'failed' });
    } finally {
      busy = false;
    }
  }

  function allowed(): boolean {
    return deps.page.visible() && !deps.page.prerendering();
  }

  function burnOutcome(error: unknown): BurnOutcome {
    if (error instanceof ApiError) {
      switch (error.kind) {
        case 'gone':
          return error.state && error.state !== 'available' ? { outcome: 'ended', state: error.state } : { outcome: 'failed' };
        case 'not_found':
          return { outcome: 'unknown' };
        case 'rate_limited':
        case 'unavailable':
          return { outcome: 'retry_later', retryAfter: error.retryAfter };
        default:
          return { outcome: 'failed' };
      }
    }
    return { outcome: 'failed' };
  }

  async function burn(): Promise<BurnOutcome> {
    const current = held;
    const shown = revealed;
    if (!current || busy || !(revealable || shown?.burnable) || !allowed()) {
      return { outcome: 'ignored' };
    }
    busy = true;
    let outcome: BurnOutcome;
    try {
      await deps.api.burn(current.id, current.readKey);
      outcome = { outcome: 'deleted' };
    } catch (error) {
      outcome = burnOutcome(error);
    } finally {
      busy = false;
    }
    if (shown) {
      if (outcome.outcome === 'deleted' || outcome.outcome === 'ended') {
        onView({ ...shown, burnable: false });
      }
    } else if (outcome.outcome === 'deleted') {
      onView(ended('deleted'));
    } else if (outcome.outcome === 'ended') {
      onView(ended(outcome.state));
    } else if (outcome.outcome === 'unknown') {
      onView({ view: 'unknown' });
    }
    return outcome;
  }

  async function report(reason: string): Promise<ReportOutcome> {
    const current = held;
    if (!current || reporting || !allowed()) {
      return { outcome: 'ignored' };
    }
    reporting = true;
    try {
      return await sendReport(deps.api, current.id, current.readKey, reason);
    } finally {
      reporting = false;
    }
  }

  return { load, reveal, burn, report };
}
