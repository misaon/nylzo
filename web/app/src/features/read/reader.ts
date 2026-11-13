import {
  assertProfile,
  deriveContent,
  deriveLink,
  idToString,
  IntegrityError,
  isReadPath,
  KdfMismatchError,
  LinkReleasedError,
  LinkRetiredKdfError,
  LinkVersionError,
  newShareKey,
  OpaqueMessageError,
  opaquePassword,
  open,
  parseFragment,
  requestKey,
  ServerAuthenticationError,
  unwrapShare,
  wipeRequestKey,
} from '@nylzo/core';
import type { ContentKeys, KdfId, Link, LinkKeys, RequestKey } from '@nylzo/core';

import { endedStateOf, isApiError, shouldRetryLater, StoppedError } from '../../shared/api/api-errors.ts';
import type { EndedState, MessageState } from '../../shared/api/api-errors.ts';
import type { Api, Envelope, Metadata } from '../../shared/api/api.ts';
import type { DownloadProgress } from '../../shared/api/object-body.ts';
import { flagsAndDeadlineMatch } from '../../shared/api/responses.ts';
import { attempt } from '../../shared/attempt.ts';
import { sanitizeFileName } from '../../shared/files/file-name.ts';
import { KdfWorkerError } from '../../shared/kdf/kdf-protocol.ts';
import type { Progress } from '../../shared/kdf/kdf.ts';
import { HANDOFF_PATH } from '../../shared/local-client/handoff.ts';
import {
  accessFor,
  attemptsOf,
  discardExportKey,
  KDF_MARK,
  kdfMark,
  passphraseRejection,
  rememberStretch,
  stretchFor,
  unlockWith,
} from './reader-login.ts';
import type { Attempts, ContentAccess, LoginTarget, PassphraseRejection, WrongPassphrase } from './reader-login.ts';
import { leaseTag } from './reader-session.ts';

export type BusyStep = 'claiming' | 'decrypting' | 'deriving' | 'downloading';

export interface RevealedFile {
  readonly name: string;
  readonly mime: string;
  readonly bytes: Uint8Array<ArrayBuffer>;
}

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
  | { readonly view: 'loading'; readonly kdfId?: KdfId | undefined }
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
  | { readonly view: 'timed_out' }
  | {
      readonly view: 'revealed';
      readonly text?: string | undefined;
      readonly file?: RevealedFile | undefined;
      readonly burnable: boolean;
      readonly opensRemaining: number;
    }
  | { readonly view: 'tampered' }
  | { readonly view: 'unknown' };

export type BurnOutcome =
  | { readonly outcome: 'deleted' }
  | { readonly outcome: 'ended'; readonly state: EndedState }
  | { readonly outcome: 'failed' }
  | { readonly outcome: 'ignored' }
  | { readonly outcome: 'rejected' }
  | { readonly outcome: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly outcome: 'unknown' }
  | { readonly outcome: 'waiting'; readonly retryAfter: number; readonly locked: boolean };

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
  readonly stretch: (message: Uint8Array<ArrayBuffer>, onProgress: Progress) => Promise<Uint8Array<ArrayBuffer>>;
  readonly now: () => number;
}

export interface Reader {
  readonly load: () => Promise<void>;
  readonly reveal: (passphrase?: string) => Promise<void>;
  readonly burn: (passphrase?: string) => Promise<BurnOutcome>;
  readonly clear: (closed?: ClosedView) => void;
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
  readonly kdfId: KdfId;
  readonly readKey: RequestKey;
}

interface Fetched {
  readonly envelope: Envelope;
  readonly lease: string;
  readonly fromLease: boolean;
}

// The fragment guard sets the same dropped keys: `r` for /r and the id for /m/{id}.
const LINK_PAGE_PATH = '/r';
const LINK_PAGE_KEY = 'r';
const RELEASED_PAGE_PREFIX = '/m/';
const LEASE_KEY = `nylzo:lease:${LINK_PAGE_KEY}`;

function isTerminal(state: MessageState): state is Exclude<EndedState, 'opened'> {
  return state === 'expired' || state === 'cancelled' || state === 'deleted';
}

function safely<Value>(action: () => Value, fallback: NoInfer<Value>): Value {
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

export type ClosedView = Extract<ReadView, { readonly view: 'cleared' | 'timed_out' }>;

const LEFT_PAGE: ClosedView = { view: 'cleared' };

function closedInstead(view: ReadView, closed: ClosedView): ReadView {
  if (view.view === 'revealed') {
    view.file?.bytes.fill(0);
  }
  return closed;
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
    served.kdfId === secrets.kdfId &&
    served.opensMax === metadata?.opensMax &&
    served.ttlSeconds === metadata.ttlSeconds &&
    flagsAndDeadlineMatch(served, metadata.expiresAt)
  );
}

function takeFragment({ location, session, ...link }: ReaderDependencies): string | ReadView {
  if (location.pathname === HANDOFF_PATH) {
    return { view: 'removed' };
  }
  const pageKey = pageKeyOf(location.pathname);
  const fragment = link.takeFragment();
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

function linkProblemView(error: unknown, pathname: string): ReadView {
  if (error instanceof LinkVersionError) {
    return { view: 'newer' };
  }
  if (error instanceof LinkReleasedError) {
    return { view: 'released' };
  }
  const pathIsValid = pageKeyOf(pathname) !== undefined;
  return pathIsValid && error instanceof LinkRetiredKdfError ? { view: 'retired' } : { view: 'invalid' };
}

function rejectionOf(attempts: Attempts, expiresAt: string | undefined, now: number): WrongPassphrase {
  const { retryAfter, attemptsLeft } = attempts;
  const expires = expiresAt ? Date.parse(expiresAt) : Number.NaN;
  const locked = attemptsLeft === 0 && Number.isFinite(expires) && now + retryAfter * 1000 >= expires;
  return { reason: 'wrong', attemptsLeft, retryAfter, locked };
}

export function createReader(dependencies: ReaderDependencies, onView: (view: ReadView) => void): Reader {
  type RevealedView = Extract<ReadView, { view: 'revealed' }>;
  const { api, session, page } = dependencies;
  let secrets: Secrets | undefined;
  let heldKey: HeldKey | undefined;
  let revealedView: RevealedView | undefined;
  let leaseOwner = '';
  let previousOwner = '';
  let metadata: Metadata | undefined;
  let deviceFailed = false;
  let busy = false;
  let acknowledging = false;
  let acknowledged: Promise<unknown> = Promise.resolve();
  let readKey: RequestKey | undefined;
  let keptPassword: Uint8Array<ArrayBuffer> | undefined;
  const remembered = rememberStretch();
  let lease: string | undefined;
  let revealable = false;
  let closedAs: ClosedView | undefined;

  const storedLease = () => lease ?? leaseOf(safely(() => session.getItem(LEASE_KEY), null));
  const allowed = () => page.visible() && !page.prerendering();
  const pageLeft = (): boolean => closedAs !== undefined;

  // Session storage holds only the lease, which authorizes nothing without the key from the link.
  function keepLease(value: string): void {
    lease = value;
    safely(() => session.setItem(LEASE_KEY, `${leaseOwner} ${value}`), undefined);
  }

  // Every link shares one page, so a stored lease names its message and serves no other.
  function leaseOf(stored: string | null): string | null {
    const [owner, value] = stored?.split(' ') ?? [];
    return (owner === leaseOwner || owner === previousOwner) && value ? value : null;
  }

  function forgetStoredLease(): void {
    safely(() => session.removeItem(LEASE_KEY), undefined);
  }

  function forgetLease(): void {
    lease = undefined;
    forgetStoredLease();
  }

  function forgetSecrets(): void {
    secrets?.secret.fill(0);
    secrets?.passphraseSalt.fill(0);
    secrets = undefined;
  }

  // A shown message keeps its OPAQUE password while its remaining opens can be deleted, since a burn runs a
  // handshake of its own (spec 6.6).
  function wipeUnusedKeys(): void {
    if (busy) {
      return;
    }
    if (revealedView?.burnable !== true) {
      keptPassword?.fill(0);
      keptPassword = undefined;
      remembered.forget();
    }
    if (!heldKey && !acknowledging) {
      wipeRequestKey(readKey);
      readKey = undefined;
    }
  }

  function show(next: ReadView): void {
    const view = closedAs ? closedInstead(next, closedAs) : next;
    revealable = view.view === 'ready';
    revealedView = view.view === 'revealed' ? view : undefined;
    if (view.view !== 'ready' && view.view !== 'busy') {
      forgetSecrets();
    }
    const keyStillNeeded = view.view === 'ready' || view.view === 'busy' || (view.view === 'revealed' && view.burnable);
    if (!keyStillNeeded) {
      heldKey = undefined;
    }
    wipeUnusedKeys();
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

  async function load(): Promise<void> {
    show({ view: 'loading' });
    const taken = takeFragment(dependencies);
    if (typeof taken !== 'string') {
      show(taken);
      return;
    }
    let link: Link;
    try {
      link = parseFragment(dependencies.location.pathname, taken);
    } catch (error) {
      show(linkProblemView(error, dependencies.location.pathname));
      return;
    }
    show({ view: 'loading', kdfId: link.kdfId });
    leaseOwner = (await attempt(leaseTag(link.secret))) ?? '';
    // The client before the lease tag stored the message id; an open it began must still finish after an upgrade.
    previousOwner = idToString(link.id);
    if (safely(() => session.getItem(KDF_MARK), null) === leaseOwner) {
      deviceFailed = true;
      safely(() => session.removeItem(KDF_MARK), undefined);
    }
    const linked = await attempt(linkSecrets(link));
    if (!linked) {
      show({ view: 'failed' });
      return;
    }
    secrets = linked;
    if (readKey && !busy && !acknowledging) {
      wipeRequestKey(readKey);
    }
    readKey = linked.readKey;
    heldKey = { id: linked.id, kdfId: linked.kdfId, readKey: linked.readKey };
    await dependencies.prepared?.();
    try {
      metadata = await api.metadata(linked.id, linked.readKey);
    } catch (error) {
      show(failureView(error));
      return;
    }
    show(metadataView(metadata, link.kdfId));
  }

  // The mark explains a tab that crashed during the stretching.
  function loginTarget(held: HeldKey, onProgress?: Progress): LoginTarget {
    const mark = kdfMark(session, () => leaseOwner);
    const stretch = remembered.around(stretchFor(held.kdfId, mark, dependencies.stretch, onProgress));
    return { api, id: held.id, kdfId: held.kdfId, readKey: held.readKey, stretch };
  }

  function rejection(attempts: Attempts): WrongPassphrase {
    return rejectionOf(attempts, metadata?.expiresAt, dependencies.now());
  }

  function passphraseFailureView(error: unknown): ReadView | undefined {
    const attempts = attemptsOf(error);
    if (attempts) {
      return readyView(rejection(attempts));
    }
    if (error instanceof ServerAuthenticationError || error instanceof OpaqueMessageError) {
      return { view: 'tampered' };
    }
    if (error instanceof KdfWorkerError) {
      deviceFailed = true;
      return readyView();
    }
    return undefined;
  }

  // A lease download needs P too: its handshake names the lease, and its KE3 takes the attempt back (spec 6.4, 6.5).
  async function fetchFromLease(
    current: Secrets,
    access: ContentAccess,
    previous: string,
    onProgress: DownloadProgress,
    onDerive: Progress,
  ): Promise<Fetched | ReadView> {
    const target = { ...loginTarget(current, onDerive), lease: previous };
    const unlock = unlockWith(target, access.password, access.keepExportKey);
    try {
      const envelope = await api.blob(current.id, current.readKey, previous, newShareKey, { onProgress, unlock });
      lease = previous;
      return { envelope, lease: previous, fromLease: true };
    } catch (error) {
      const passphraseView = passphraseFailureView(error);
      if (passphraseView) {
        return passphraseView;
      }
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
    onDerive: Progress,
  ): Promise<Fetched | ReadView> {
    show({ view: 'busy', step: 'claiming' });
    const unlock = unlockWith(loginTarget(current, onDerive), access.password, access.keepExportKey);
    try {
      const claimed = await api.claim(current.id, current.readKey, {
        unlock: async () => {
          const login = await unlock();
          show({ view: 'busy', step: 'claiming' });
          return login;
        },
        shareKeys: newShareKey,
        onLease: keepLease,
        onProgress,
        proceed: () => !pageLeft(),
      });
      return { envelope: claimed, lease: claimed.lease, fromLease: false };
    } catch (error) {
      if (error instanceof StoppedError) {
        return { view: 'cleared' };
      }
      return passphraseFailureView(error) ?? failureView(error);
    }
  }

  function showDownload(received: number, total: number | undefined): void {
    show({ view: 'busy', step: 'downloading', fraction: total ? received / total : undefined });
  }

  async function fetchEnvelope(
    current: Secrets,
    access: ContentAccess,
    onDerive: Progress,
  ): Promise<Fetched | ReadView> {
    const onProgress = showDownload;
    const previous = storedLease();
    return previous === null
      ? claim(current, access, onProgress, onDerive)
      : fetchFromLease(current, access, previous, onProgress, onDerive);
  }

  // Every claim follows a handshake, also without a passphrase, so its requests do not tell (spec 2).
  function contentAccess(current: Secrets, passphrase: string): ContentAccess | ReadView {
    try {
      return accessFor(opaquePassword(current.passphraseSalt, current.kdfId, passphrase));
    } catch (error) {
      const rejected = passphraseRejection(error);
      return rejected === undefined ? { view: 'failed' } : readyView(rejected);
    }
  }

  // The content key needs the share, which only the server's answer brings.
  async function contentKeys(current: Secrets, access: ContentAccess, fetched: Fetched): Promise<ContentKeys> {
    const share = await unwrapShare(fetched.envelope.shareKey, fetched.envelope.share);
    try {
      const exportKey = access.exportKey();
      if (exportKey === undefined) {
        throw new IntegrityError('the handshake gave no P');
      }
      return await deriveContent(current.id, current.secret, exportKey, share);
    } finally {
      share.fill(0);
    }
  }

  async function acknowledge(
    current: Secrets,
    fetched: Fetched,
    challenge: Promise<Uint8Array<ArrayBuffer> | undefined>,
  ): Promise<void> {
    await attempt(api.ack(current.id, current.readKey, fetched.lease, await challenge));
    acknowledging = false;
    lease = undefined;
    wipeUnusedKeys();
  }

  async function revealOnce(current: Secrets, passphrase: string): Promise<ReadView | undefined> {
    const access = contentAccess(current, passphrase);
    if ('view' in access) {
      return access;
    }
    try {
      return await revealWith(current, access);
    } finally {
      access.exportKey()?.fill(0);
      if (access.password !== keptPassword) {
        access.password.fill(0);
      }
    }
  }

  async function revealWith(current: Secrets, access: ContentAccess): Promise<ReadView | undefined> {
    if (pageLeft()) {
      return { view: 'cleared' };
    }
    const fetched = await fetchEnvelope(current, access, (fraction) => {
      show({ view: 'busy', step: 'deriving', fraction });
    });
    if ('view' in fetched) {
      return fetched;
    }
    if (!policyMatches(fetched.envelope, current, metadata)) {
      fetched.envelope.shareKey.seed.fill(0);
      return { view: 'tampered' };
    }
    const ackChallenge = attempt(api.challenge());
    show({ view: 'busy', step: 'decrypting' });
    let opened: Awaited<ReturnType<typeof open>>;
    try {
      const { opensMax, ttlSeconds, deadline } = fetched.envelope.policy;
      const keys = await contentKeys(current, access, fetched);
      opened = await open(
        keys,
        {
          id: current.id,
          policy: { opensMax, ttlSeconds, ...(deadline !== undefined && { deadline }) },
          kdfId: current.kdfId,
          objectHash: current.objectHash,
        },
        fetched.envelope.object,
      );
    } catch {
      return { view: 'tampered' };
    }
    if (pageLeft()) {
      opened.payload.file?.fill(0);
      return { view: 'cleared' };
    }
    const { text, file } = opened.payload.meta;
    const remaining = (metadata?.opensRemaining ?? 0) - (fetched.fromLease ? 0 : 1);
    if (remaining > 0) {
      keptPassword?.fill(0);
      keptPassword = access.password;
    }
    show({
      view: 'revealed',
      text,
      file:
        file && opened.payload.file
          ? { name: sanitizeFileName(file.name), mime: file.mime, bytes: opened.payload.file }
          : undefined,
      burnable: remaining > 0,
      opensRemaining: Math.max(0, remaining),
    });
    await dependencies.shown?.();
    if (pageLeft()) {
      return undefined;
    }
    // Once the message is on the screen, no reload may bring it back from the lease, even if the ack never arrives.
    forgetStoredLease();
    acknowledging = true;
    acknowledged = acknowledge(current, fetched, ackChallenge);
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
      wipeUnusedKeys();
    }
  }

  function showBurnOutcome(outcome: BurnOutcome, shown: RevealedView | undefined): void {
    if (shown) {
      if (revealedView === shown && (outcome.outcome === 'deleted' || outcome.outcome === 'ended')) {
        show({ ...shown, burnable: false, opensRemaining: 0 });
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
      case 'rejected':
      case 'retry_later':
      case 'waiting': {
        break;
      }
    }
  }

  async function sendBurn(current: HeldKey, password: Uint8Array): Promise<BurnOutcome> {
    try {
      await api.burn(current.id, current.readKey, unlockWith(loginTarget(current), password, discardExportKey));
      return { outcome: 'deleted' };
    } catch (error) {
      const attempts = attemptsOf(error);
      if (!attempts) {
        return burnOutcomeOf(error);
      }
      const refusal = rejection(attempts);
      // A shown message runs its handshake with the passphrase that opened it, so its refusal is only the backoff of
      // others' wrong attempts.
      if (!revealable) {
        return { outcome: 'waiting', retryAfter: refusal.retryAfter, locked: refusal.locked };
      }
      show(readyView(refusal));
      return { outcome: 'rejected' };
    }
  }

  async function burnUnopened(current: HeldKey, unopened: Secrets, passphrase: string): Promise<BurnOutcome> {
    const access = contentAccess(unopened, passphrase);
    if ('view' in access) {
      if (access.view !== 'ready') {
        return { outcome: 'failed' };
      }
      show(access);
      return { outcome: 'rejected' };
    }
    try {
      return pageLeft() ? { outcome: 'ignored' } : await sendBurn(current, access.password);
    } finally {
      access.password.fill(0);
    }
  }

  async function burn(passphrase = ''): Promise<BurnOutcome> {
    const current = heldKey;
    const shown = revealedView;
    const unopened = revealable ? secrets : undefined;
    if (!current || busy || !(unopened || shown?.burnable) || !allowed()) {
      return { outcome: 'ignored' };
    }
    busy = true;
    let outcome: BurnOutcome;
    try {
      await acknowledged;
      if (unopened) {
        outcome = await burnUnopened(current, unopened, passphrase);
      } else {
        outcome = keptPassword ? await sendBurn(current, keptPassword) : { outcome: 'failed' };
      }
    } finally {
      busy = false;
      wipeUnusedKeys();
    }
    showBurnOutcome(outcome, shown);
    return outcome;
  }

  function clear(closed = LEFT_PAGE): void {
    closedAs = closed;
    revealedView?.file?.bytes.fill(0);
    show(closed);
  }

  return { load, reveal, burn, clear };
}
