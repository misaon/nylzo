import {
  assertProfile,
  assertWellFormedPassphrase,
  type ContentKeys,
  deriveContent,
  deriveLink,
  deriveVerifier,
  idToString,
  IntegrityError,
  isReadPath,
  KDF_NONE,
  type KdfId,
  KdfMismatchError,
  type Link,
  type LinkKeys,
  LinkReleasedError,
  LinkRetiredKdfError,
  LinkVersionError,
  MalformedPassphraseError,
  newShareKey,
  open,
  parseFragment,
  requestKey,
  type RequestKey,
  type ShareKey,
  unwrapShare,
  wipeRequestKey,
} from '@nylzo/core';

import type { AbuseReport, Api, Envelope, Metadata } from '../../shared/api/api.ts';
import type { DownloadProgress } from '../../shared/api/object-body.ts';
import type { Progress } from '../../shared/kdf/kdf.ts';

import {
  type ApiError,
  type EndedState,
  endedStateOf,
  isApiError,
  type MessageState,
  shouldRetryLater,
  StoppedError,
} from '../../shared/api/api-errors.ts';
import { attempt } from '../../shared/attempt.ts';
import { sanitizeFileName } from '../../shared/files/file-name.ts';
import { KdfWorkerError } from '../../shared/kdf/kdf-protocol.ts';
import { HANDOFF_PATH } from '../../shared/local-client/handoff.ts';
import { type ReportOutcome, sendReport } from '../../shared/report/report.ts';

export type BusyStep = 'claiming' | 'decrypting' | 'deriving' | 'downloading';

export interface RevealedFile {
  readonly name: string;
  readonly mime: string;
  readonly bytes: Uint8Array<ArrayBuffer>;
}

type PassphraseRejection =
  | { readonly reason: 'malformed' }
  | { readonly reason: 'wrong_or_changed' }
  | { readonly reason: 'wrong'; readonly attemptsLeft: number; readonly retryAfter: number; readonly locked: boolean };

export type ReadView =
  | { readonly view: 'busy'; readonly step: BusyStep; readonly fraction?: number | undefined }
  | { readonly view: 'cleared' }
  | {
      readonly view: 'ended';
      readonly state: EndedState;
      readonly firstOpenedAt?: string | undefined;
      readonly endedAt?: string | undefined;
    }
  | { readonly view: 'failed' }
  | { readonly view: 'invalid' }
  | { readonly view: 'loading' }
  | { readonly view: 'newer' }
  | {
      readonly view: 'ready';
      readonly metadata: Metadata;
      readonly resumable: boolean;
      readonly deviceFailed: boolean;
      readonly leaseEnded?: boolean | undefined;
      readonly rejected?: PassphraseRejection | undefined;
    }
  | { readonly view: 'released' }
  | { readonly view: 'removed' }
  | { readonly view: 'reopen' }
  | { readonly view: 'retired' }
  | { readonly view: 'retry_later'; readonly retryAfter?: number | undefined }
  | {
      readonly view: 'revealed';
      readonly text?: string | undefined;
      readonly file?: RevealedFile | undefined;
      readonly burnable: boolean;
    }
  | { readonly view: 'tampered' }
  | { readonly view: 'unknown' };

export type BurnOutcome =
  | { readonly outcome: 'deleted' }
  | { readonly outcome: 'ended'; readonly state: EndedState }
  | { readonly outcome: 'failed' }
  | { readonly outcome: 'ignored' }
  | { readonly outcome: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly outcome: 'unknown' };

export interface SessionStore {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
  removeItem: (key: string) => void;
}

export interface ReaderDependencies {
  readonly api: Api;
  readonly location: { readonly pathname: string };
  readonly takeFragment: () => string;
  readonly session: SessionStore;
  readonly page: { readonly visible: () => boolean; readonly prerendering: () => boolean };
  readonly prepared?: () => Promise<void>;
  readonly shown?: () => Promise<void>;
  readonly scrypt: (
    passphrase: string,
    passphraseSalt: Uint8Array<ArrayBuffer>,
    onProgress: Progress,
  ) => Promise<Uint8Array<ArrayBuffer>>;
  readonly now: () => number;
}

export interface Reader {
  readonly load: () => Promise<void>;
  readonly reveal: (passphrase?: string) => Promise<void>;
  readonly burn: () => Promise<BurnOutcome>;
  readonly report: (report: AbuseReport) => Promise<ReportOutcome>;
  readonly clear: () => void;
}

interface Secrets {
  readonly id: Uint8Array<ArrayBuffer>;
  readonly kdfId: KdfId;
  readonly secret: Uint8Array<ArrayBuffer>;
  readonly objectHash: Uint8Array<ArrayBuffer>;
  readonly passphraseSalt: Uint8Array<ArrayBuffer>;
  readonly readKey: RequestKey;
}

interface HeldKey {
  readonly id: Uint8Array<ArrayBuffer>;
  readonly readKey: RequestKey;
}

interface Fetched {
  readonly envelope: Envelope;
  readonly lease: string;
  readonly fromLease: boolean;
}

// The passphrase key waits as bytes until the claim brings the share that the content key also needs.
interface ContentAccess {
  readonly passphraseKey: Uint8Array<ArrayBuffer>;
  readonly verifierKey: RequestKey;
  readonly shareKey: ShareKey;
}

// The fragment guard sets the same dropped keys: `r` for /r and the id for /m/{id}.
const LINK_PAGE_PATH = '/r';
const LINK_PAGE_KEY = 'r';
const RELEASED_PAGE_PREFIX = '/m/';
const LEASE_KEY = `nylzo:lease:${LINK_PAGE_KEY}`;

function isTerminal(state: MessageState): state is Exclude<EndedState, 'opened'> {
  return state === 'expired' || state === 'cancelled' || state === 'deleted';
}

function safely<Value>(action: () => Value, fallback: Value): Value {
  try {
    return action();
  } catch {
    return fallback;
  }
}

function profileMatches(metadata: Metadata, kdfId: KdfId): boolean {
  try {
    assertProfile(kdfId, metadata.kdfId);
  } catch (error) {
    if (error instanceof KdfMismatchError) {
      return false;
    }
    throw error;
  }
  return true;
}

function pageKeyOf(path: string): string | undefined {
  if (!isReadPath(path)) {
    return undefined;
  }
  return path === LINK_PAGE_PATH ? LINK_PAGE_KEY : path.slice(RELEASED_PAGE_PREFIX.length);
}

async function linkSecrets(link: Link): Promise<Secrets> {
  let keys: LinkKeys | undefined;
  try {
    keys = await deriveLink(link.id, link.secret);
    const readKey = await requestKey(keys.readSeed);
    return {
      id: link.id,
      kdfId: link.kdfId,
      secret: link.secret,
      objectHash: link.objectHash,
      passphraseSalt: keys.passphraseSalt,
      readKey,
    };
  } catch (error) {
    link.secret.fill(0);
    keys?.passphraseSalt.fill(0);
    throw error;
  } finally {
    keys?.readSeed.fill(0);
  }
}

function cleared(view: ReadView): ReadView {
  if (view.view === 'revealed') {
    view.file?.bytes.fill(0);
  }
  return { view: 'cleared' };
}

function burnOutcomeOf(error: unknown): BurnOutcome {
  if (isApiError(error, 'gone')) {
    const state = endedStateOf(error);
    return state ? { outcome: 'ended', state } : { outcome: 'failed' };
  }
  if (isApiError(error, 'not_found')) {
    return { outcome: 'unknown' };
  }
  return shouldRetryLater(error) ? { outcome: 'retry_later', retryAfter: error.retryAfter } : { outcome: 'failed' };
}

function policyMatches(envelope: Envelope, secrets: Secrets, metadata: Metadata | undefined): boolean {
  const served = envelope.policy;
  return (
    served.flags === 0 &&
    served.kdfId === secrets.kdfId &&
    served.opensMax === metadata?.opensMax &&
    served.ttlSeconds === metadata.ttlSeconds
  );
}

export function createReader(dependencies: ReaderDependencies, onView: (view: ReadView) => void): Reader {
  type RevealedView = Extract<ReadView, { view: 'revealed' }>;
  const { api, session, page } = dependencies;
  let secrets: Secrets | undefined;
  let heldKey: HeldKey | undefined;
  let revealedView: RevealedView | undefined;
  let messageId = '';
  let metadata: Metadata | undefined;
  let deviceFailed = false;
  let busy = false;
  let reporting = false;
  let acknowledging = false;
  let acknowledged: Promise<unknown> = Promise.resolve();
  let readKey: RequestKey | undefined;
  let lease: string | undefined;
  let revealable = false;
  let left = false;

  const kdfKey = () => `nylzo:kdf:${messageId}`;
  const storedLease = () => lease ?? leaseOf(safely(() => session.getItem(LEASE_KEY), null));
  const allowed = () => page.visible() && !page.prerendering();
  const pageLeft = (): boolean => left;

  // Session storage holds only the lease, which authorizes nothing without the key from the link.
  function keepLease(value: string): void {
    lease = value;
    safely(() => session.setItem(LEASE_KEY, `${messageId} ${value}`), undefined);
  }

  // Every link shares one page, so a stored lease names its message and serves no other.
  function leaseOf(stored: string | null): string | null {
    const [owner, value] = stored?.split(' ') ?? [];
    return owner === messageId && value ? value : null;
  }

  function forgetLease(): void {
    lease = undefined;
    safely(() => session.removeItem(LEASE_KEY), undefined);
  }

  function takeFragment(): string | ReadView {
    if (dependencies.location.pathname === HANDOFF_PATH) {
      return { view: 'removed' };
    }
    const pageKey = pageKeyOf(dependencies.location.pathname);
    const fragment = dependencies.takeFragment();
    if (pageKey === undefined) {
      return fragment;
    }
    const droppedKey = `nylzo:dropped:${pageKey}`;
    if (fragment !== '') {
      safely(() => session.setItem(droppedKey, '1'), undefined);
      return fragment;
    }
    if (safely(() => session.getItem(`nylzo:lease:${pageKey}`), null) !== null) {
      return { view: 'reopen' };
    }
    return safely(() => session.getItem(droppedKey), null) === null ? fragment : { view: 'removed' };
  }

  function forgetSecrets(): void {
    secrets?.secret.fill(0);
    secrets?.passphraseSalt.fill(0);
    secrets = undefined;
  }

  function wipeUnusedReadKey(): void {
    if (heldKey || busy || reporting || acknowledging) {
      return;
    }
    wipeRequestKey(readKey);
    readKey = undefined;
  }

  function show(next: ReadView): void {
    const view = left ? cleared(next) : next;
    revealable = view.view === 'ready';
    revealedView = view.view === 'revealed' ? view : undefined;
    if (view.view !== 'ready' && view.view !== 'busy') {
      forgetSecrets();
    }
    const keyStillNeeded = view.view === 'ready' || view.view === 'busy' || (view.view === 'revealed' && view.burnable);
    if (!keyStillNeeded) {
      heldKey = undefined;
    }
    wipeUnusedReadKey();
    onView(view);
  }

  function endedView(state: EndedState, times: Pick<Metadata, 'endedAt' | 'firstOpenedAt'> = {}): ReadView {
    forgetLease();
    return { view: 'ended', state, firstOpenedAt: times.firstOpenedAt, endedAt: times.endedAt };
  }

  function failureView(error: unknown): ReadView {
    if (isApiError(error, 'not_found')) {
      forgetLease();
      return { view: 'unknown' };
    }
    if (isApiError(error, 'gone')) {
      const state = endedStateOf(error);
      return state ? endedView(state) : { view: 'failed' };
    }
    return shouldRetryLater(error) ? { view: 'retry_later', retryAfter: error.retryAfter } : { view: 'failed' };
  }

  function readyView(rejected?: PassphraseRejection, leaseEnded?: boolean): ReadView {
    return metadata && secrets
      ? { view: 'ready', metadata, resumable: storedLease() !== null, deviceFailed, leaseEnded, rejected }
      : { view: 'failed' };
  }

  function metadataView(current: Metadata, kdfId: KdfId): ReadView {
    if (current.endedAt !== undefined) {
      return current.state === 'available' ? { view: 'failed' } : endedView(current.state, current);
    }
    if (isTerminal(current.state)) {
      return endedView(current.state, current);
    }
    if (!profileMatches(current, kdfId)) {
      return { view: 'tampered' };
    }
    if (current.state === 'opened' && storedLease() === null) {
      return endedView('opened', current);
    }
    return readyView();
  }

  function linkProblemView(error: unknown): ReadView {
    if (error instanceof LinkVersionError) {
      return { view: 'newer' };
    }
    if (error instanceof LinkReleasedError) {
      return { view: 'released' };
    }
    const pathIsValid = pageKeyOf(dependencies.location.pathname) !== undefined;
    return pathIsValid && error instanceof LinkRetiredKdfError ? { view: 'retired' } : { view: 'invalid' };
  }

  async function load(): Promise<void> {
    show({ view: 'loading' });
    const taken = takeFragment();
    if (typeof taken !== 'string') {
      show(taken);
      return;
    }
    let link: Link;
    try {
      link = parseFragment(dependencies.location.pathname, taken);
    } catch (error) {
      show(linkProblemView(error));
      return;
    }
    messageId = idToString(link.id);
    if (safely(() => session.getItem(kdfKey()), null) !== null) {
      deviceFailed = true;
      safely(() => session.removeItem(kdfKey()), undefined);
    }
    const linked = await attempt(linkSecrets(link));
    if (!linked) {
      show({ view: 'failed' });
      return;
    }
    secrets = linked;
    if (readKey && !busy && !reporting && !acknowledging) {
      wipeRequestKey(readKey);
    }
    readKey = linked.readKey;
    heldKey = { id: linked.id, readKey: linked.readKey };
    await dependencies.prepared?.();
    try {
      metadata = await api.metadata(linked.id, linked.readKey);
    } catch (error) {
      show(failureView(error));
      return;
    }
    show(metadataView(metadata, link.kdfId));
  }

  async function derivePassphraseKey(current: Secrets, passphrase: string): Promise<Uint8Array<ArrayBuffer>> {
    if (current.kdfId === KDF_NONE) {
      return new Uint8Array(0);
    }
    show({ view: 'busy', step: 'deriving', fraction: 0 });
    safely(() => session.setItem(kdfKey(), '1'), undefined);
    try {
      return await dependencies.scrypt(passphrase, current.passphraseSalt, (fraction) =>
        show({ view: 'busy', step: 'deriving', fraction }),
      );
    } finally {
      safely(() => session.removeItem(kdfKey()), undefined);
    }
  }

  function rejection(error: ApiError): PassphraseRejection {
    const retryAfter = error.retryAfter ?? 0;
    const attemptsLeft = error.attemptsLeft ?? 0;
    const expiresAt = metadata?.expiresAt ? Date.parse(metadata.expiresAt) : NaN;
    const locked =
      attemptsLeft === 0 && Number.isFinite(expiresAt) && dependencies.now() + retryAfter * 1000 >= expiresAt;
    return { reason: 'wrong', attemptsLeft, retryAfter, locked };
  }

  async function fetchFromLease(
    current: Secrets,
    previous: string,
    shareKey: ShareKey,
    onProgress: DownloadProgress,
  ): Promise<Fetched | ReadView> {
    try {
      const envelope = await api.blob(current.id, current.readKey, previous, shareKey.publicKey, onProgress);
      lease = previous;
      return { envelope, lease: previous, fromLease: true };
    } catch (error) {
      if (!isApiError(error, 'gone')) {
        return failureView(error);
      }
      forgetLease();
      return error.state === 'available' ? readyView(undefined, true) : failureView(error);
    }
  }

  async function claim(
    current: Secrets,
    access: ContentAccess,
    onProgress: DownloadProgress,
  ): Promise<Fetched | ReadView> {
    show({ view: 'busy', step: 'claiming' });
    try {
      const claimed = await api.claim(current.id, current.readKey, {
        verifierKey: access.verifierKey,
        shareKey: access.shareKey.publicKey,
        onLease: keepLease,
        onProgress,
        proceed: () => !left,
      });
      return { envelope: claimed, lease: claimed.lease, fromLease: false };
    } catch (error) {
      if (error instanceof StoppedError) {
        return { view: 'cleared' };
      }
      if (isApiError(error, 'passphrase')) {
        return readyView(rejection(error));
      }
      return failureView(error);
    }
  }

  function showDownload(received: number, total: number | undefined): void {
    show({ view: 'busy', step: 'downloading', fraction: total ? received / total : undefined });
  }

  async function fetchEnvelope(current: Secrets, access: ContentAccess): Promise<Fetched | ReadView> {
    const onProgress = showDownload;
    const previous = storedLease();
    return previous === null
      ? claim(current, access, onProgress)
      : fetchFromLease(current, previous, access.shareKey, onProgress);
  }

  // Every claim carries a verifier, also without a passphrase, so its headers do not tell (spec 2).
  async function contentAccess(current: Secrets, passphrase: string): Promise<ContentAccess | ReadView> {
    let passphraseKey: Uint8Array<ArrayBuffer> | undefined;
    let verifierSeed: Uint8Array | undefined;
    try {
      assertWellFormedPassphrase(current.kdfId, passphrase);
      passphraseKey = await derivePassphraseKey(current, passphrase);
      verifierSeed = await deriveVerifier(current.id, current.secret, passphraseKey);
      const verifierKey = await requestKey(verifierSeed);
      return { passphraseKey, verifierKey, shareKey: await newShareKey() };
    } catch (error) {
      passphraseKey?.fill(0);
      if (error instanceof MalformedPassphraseError) {
        return readyView({ reason: 'malformed' });
      }
      if (error instanceof KdfWorkerError) {
        deviceFailed = true;
        return readyView();
      }
      return { view: 'failed' };
    } finally {
      verifierSeed?.fill(0);
    }
  }

  // The content key needs the share, which only the server's answer brings.
  async function contentKeys(current: Secrets, access: ContentAccess, fetched: Fetched): Promise<ContentKeys> {
    if (fetched.envelope.share === undefined) {
      throw new IntegrityError('the server sent no share');
    }
    const share = await unwrapShare(access.shareKey, fetched.envelope.share);
    try {
      return await deriveContent(current.id, current.secret, access.passphraseKey, share);
    } finally {
      share.fill(0);
    }
  }

  async function acknowledge(current: Secrets, fetched: Fetched): Promise<void> {
    await attempt(api.ack(current.id, current.readKey, fetched.lease));
    acknowledging = false;
    forgetLease();
    wipeUnusedReadKey();
  }

  async function revealOnce(current: Secrets, passphrase: string): Promise<ReadView | undefined> {
    const access = await contentAccess(current, passphrase);
    if ('view' in access) {
      return access;
    }
    try {
      return await revealWith(current, access);
    } finally {
      access.passphraseKey.fill(0);
    }
  }

  async function revealWith(current: Secrets, access: ContentAccess): Promise<ReadView | undefined> {
    if (pageLeft()) {
      wipeRequestKey(access.verifierKey);
      return { view: 'cleared' };
    }
    let fetched: Fetched | ReadView;
    try {
      fetched = await fetchEnvelope(current, access);
    } finally {
      wipeRequestKey(access.verifierKey);
    }
    if ('view' in fetched) {
      return fetched;
    }
    if (!policyMatches(fetched.envelope, current, metadata)) {
      return { view: 'tampered' };
    }
    show({ view: 'busy', step: 'decrypting' });
    let opened: Awaited<ReturnType<typeof open>>;
    try {
      const { opensMax, ttlSeconds } = fetched.envelope.policy;
      const keys = await contentKeys(current, access, fetched);
      opened = await open(
        keys,
        {
          id: current.id,
          policy: { opensMax, ttlSeconds },
          kdfId: current.kdfId,
          objectHash: current.objectHash,
        },
        fetched.envelope.object,
      );
    } catch {
      return fetched.fromLease && current.kdfId !== KDF_NONE
        ? readyView({ reason: 'wrong_or_changed' })
        : { view: 'tampered' };
    }
    if (pageLeft()) {
      opened.payload.file?.fill(0);
      return { view: 'cleared' };
    }
    const { text, file } = opened.payload.meta;
    const remaining = (metadata?.opensRemaining ?? 0) - (fetched.fromLease ? 0 : 1);
    show({
      view: 'revealed',
      text,
      file:
        file && opened.payload.file
          ? { name: sanitizeFileName(file.name), mime: file.mime, bytes: opened.payload.file }
          : undefined,
      burnable: remaining > 0,
    });
    await dependencies.shown?.();
    if (pageLeft()) {
      return undefined;
    }
    acknowledging = true;
    acknowledged = acknowledge(current, fetched);
    return undefined;
  }

  async function reveal(passphrase = ''): Promise<void> {
    const current = secrets;
    if (!current || !revealable || busy || !allowed()) {
      return;
    }
    busy = true;
    try {
      const view = await revealOnce(current, passphrase);
      if (view) {
        show(view);
      }
    } catch {
      show({ view: 'failed' });
    } finally {
      busy = false;
      wipeUnusedReadKey();
    }
  }

  function showBurnOutcome(outcome: BurnOutcome, shown: RevealedView | undefined): void {
    if (shown) {
      if (revealedView === shown && (outcome.outcome === 'deleted' || outcome.outcome === 'ended')) {
        show({ ...shown, burnable: false });
      }
      return;
    }
    switch (outcome.outcome) {
      case 'deleted': {
        show(endedView('deleted'));
        break;
      }
      case 'ended': {
        show(endedView(outcome.state));
        break;
      }
      case 'unknown': {
        show({ view: 'unknown' });
        break;
      }
      case 'failed':
      case 'ignored':
      case 'retry_later': {
        break;
      }
    }
  }

  async function burn(): Promise<BurnOutcome> {
    const current = heldKey;
    const shown = revealedView;
    if (!current || busy || !(revealable || shown?.burnable) || !allowed()) {
      return { outcome: 'ignored' };
    }
    busy = true;
    let outcome: BurnOutcome;
    try {
      await acknowledged;
      await api.burn(current.id, current.readKey);
      outcome = { outcome: 'deleted' };
    } catch (error) {
      outcome = burnOutcomeOf(error);
    } finally {
      busy = false;
      wipeUnusedReadKey();
    }
    showBurnOutcome(outcome, shown);
    return outcome;
  }

  async function report(abuseReport: AbuseReport): Promise<ReportOutcome> {
    const current = heldKey;
    if (!current || reporting || revealedView?.burnable === false || !allowed()) {
      return { outcome: 'ignored' };
    }
    reporting = true;
    try {
      return await sendReport(api, current.id, current.readKey, abuseReport);
    } finally {
      reporting = false;
      if (current.readKey !== readKey) {
        wipeRequestKey(current.readKey);
      }
      wipeUnusedReadKey();
    }
  }

  function clear(): void {
    left = true;
    revealedView?.file?.bytes.fill(0);
    show({ view: 'cleared' });
  }

  return { load, reveal, burn, report, clear };
}
