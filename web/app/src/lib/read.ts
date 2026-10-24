import {
  KDF_NONE,
  KDF_PBKDF2,
  KdfMismatchError,
  MalformedPassphraseError,
  assertProfile,
  assertWellFormedPassphrase,
  deriveContent,
  deriveLink,
  derivePassphraseKey,
  idToString,
  open,
  parseFragment,
  parsePath,
  requestKey,
  type KdfId,
  type Link,
  type LinkKeys,
  wipeRequestKey,
  type RequestKey,
} from '@nylzo/core';

import { ApiError, StoppedError, type Api, type Envelope, type MessageState, type Metadata } from './api.ts';
import { sanitizeFileName } from './download.ts';
import { KdfWorkerError } from './kdf-protocol.ts';
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
  | { readonly view: 'cleared' }
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
      readonly leaseEnded?: boolean | undefined;
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
  clear(): void;
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

interface ContentAccess {
  readonly k: CryptoKey;
  readonly verifierKey: RequestKey | undefined;
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
  let acknowledged: Promise<void> = Promise.resolve();
  let acknowledging = false;
  let readKey: RequestKey | undefined;
  let lease: string | undefined;
  let revealable = false;
  let left = false;

  const onView = (next: ReadView) => {
    const view = left ? cleared(next) : next;
    revealable = view.view === 'ready';
    revealed = view.view === 'revealed' ? view : undefined;
    if (view.view !== 'ready' && view.view !== 'busy') {
      forget();
    }
    if (view.view !== 'ready' && view.view !== 'busy' && view.view !== 'revealed') {
      held = undefined;
    }
    wipeUnused();
    notify(view);
  };

  function wipeUnused(): void {
    if (!held && !busy && !reporting && !acknowledging) {
      wipeRequestKey(readKey);
      readKey = undefined;
    }
  }

  function cleared(view: ReadView): ReadView {
    if (view.view === 'revealed') {
      view.file?.bytes.fill(0);
    }
    return { view: 'cleared' };
  }

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

  function readyView(rejected?: PassphraseRejection, leaseEnded?: boolean): ReadView {
    if (!metadata || !secrets) {
      return { view: 'failed' };
    }
    return { view: 'ready', metadata, resumable: storedLease() !== null, deviceFailed, leaseEnded, rejected };
  }

  function profileMatches(current: Metadata, kdfId: KdfId): boolean {
    try {
      assertProfile(kdfId, current.kdfId);
    } catch (error) {
      if (error instanceof KdfMismatchError) {
        return false;
      }
      throw error;
    }
    return current.passphraseRequired === (kdfId !== KDF_NONE);
  }

  function metadataView(current: Metadata, kdfId: KdfId): ReadView {
    if (current.endedAt !== undefined) {
      return current.state === 'available' ? { view: 'failed' } : ended(current.state, current);
    }
    if (isTerminal(current.state)) {
      return ended(current.state, current);
    }
    if (!profileMatches(current, kdfId)) {
      return { view: 'tampered' };
    }
    if (current.state === 'opened' && storedLease() === null) {
      return ended('opened', current);
    }
    return readyView();
  }

  async function linkSecrets(fragment: Link): Promise<Secrets> {
    let keys: LinkKeys | undefined;
    try {
      keys = await deriveLink(fragment.id, fragment.s);
      const readKey = await requestKey(keys.r);
      return { id: fragment.id, kdfId: fragment.kdfId, s: fragment.s, saltPw: keys.saltPw, readKey };
    } catch (error) {
      fragment.s.fill(0);
      keys?.saltPw.fill(0);
      throw error;
    } finally {
      keys?.r.fill(0);
    }
  }

  async function load(): Promise<void> {
    onView({ view: 'loading' });
    let fragment: Link;
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
    const linked = await linkSecrets(fragment).catch(() => undefined);
    if (!linked) {
      onView({ view: 'failed' });
      return;
    }
    secrets = linked;
    readKey = linked.readKey;
    held = { id: linked.id, readKey: linked.readKey };
    try {
      metadata = await deps.api.metadata(linked.id, linked.readKey);
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
        return error.state === 'available' ? readyView(undefined, true) : apiView(error);
      }
    }
    onView({ view: 'busy', step: 'claiming' });
    try {
      const claimed = await deps.api.claim(current.id, current.readKey, { verifierKey, onLease: keepLease, onProgress, proceed: () => !left });
      return { envelope: claimed, lease: claimed.lease, fromLease: false };
    } catch (error) {
      if (error instanceof StoppedError) {
        return { view: 'cleared' };
      }
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

  async function contentAccess(current: Secrets, passphrase: string): Promise<ContentAccess | ReadView> {
    let p: Uint8Array | undefined;
    let v: Uint8Array | undefined;
    try {
      assertWellFormedPassphrase(current.kdfId, passphrase);
      p = await passphraseKey(current, passphrase);
      const content = await deriveContent(current.id, current.s, p);
      v = content.v;
      const verifierKey = current.kdfId === KDF_NONE ? undefined : await requestKey(content.v);
      return { k: content.k, verifierKey };
    } catch (error) {
      if (error instanceof MalformedPassphraseError) {
        return readyView({ reason: 'malformed' });
      }
      if (error instanceof KdfWorkerError) {
        deviceFailed = true;
        return readyView();
      }
      return { view: 'failed' };
    } finally {
      p?.fill(0);
      v?.fill(0);
    }
  }

  async function revealOnce(current: Secrets, passphrase: string): Promise<ReadView | undefined> {
    const access = await contentAccess(current, passphrase);
    if ('view' in access) {
      return access;
    }
    if (left) {
      return { view: 'cleared' };
    }
    const fetched = await fetchEnvelope(current, access.verifierKey).finally(() => wipeRequestKey(access.verifierKey));
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
      opened = await open(access.k, current.id, policy, current.kdfId, fetched.envelope.object);
    } catch {
      return fetched.fromLease && current.kdfId !== KDF_NONE ? readyView({ reason: 'wrong_or_changed' }) : { view: 'tampered' };
    }
    if (left) {
      opened.payload.file?.fill(0);
      return { view: 'cleared' };
    }
    deps.dropFragment();
    const { text, file } = opened.payload.meta;
    const remaining = (metadata?.opensRemaining ?? 0) - (fetched.fromLease ? 0 : 1);
    onView({
      view: 'revealed',
      text,
      file: file && opened.payload.file ? { name: sanitizeFileName(file.name), mime: file.mime, bytes: opened.payload.file } : undefined,
      burnable: remaining > 0,
    });
    acknowledging = true;
    acknowledged = deps.api
      .ack(current.id, current.readKey, fetched.lease)
      .catch(() => undefined)
      .then(() => {
        acknowledging = false;
        forgetLease();
        wipeUnused();
      });
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
      wipeUnused();
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
      await acknowledged;
      await deps.api.burn(current.id, current.readKey);
      outcome = { outcome: 'deleted' };
    } catch (error) {
      outcome = burnOutcome(error);
    } finally {
      busy = false;
      wipeUnused();
    }
    if (shown) {
      if (revealed === shown && (outcome.outcome === 'deleted' || outcome.outcome === 'ended')) {
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
    if (!current || reporting || revealed?.burnable === false || !allowed()) {
      return { outcome: 'ignored' };
    }
    reporting = true;
    try {
      return await sendReport(deps.api, current.id, current.readKey, reason);
    } finally {
      reporting = false;
      wipeUnused();
    }
  }

  function clear(): void {
    left = true;
    revealed?.file?.bytes.fill(0);
    onView({ view: 'cleared' });
  }

  return { load, reveal, burn, report, clear };
}
