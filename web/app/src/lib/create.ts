import {
  KDF_NONE,
  KDF_SCRYPT,
  MAX_MIME,
  MAX_TEXT_BYTES,
  MIN_TTL_SECONDS,
  MalformedPassphraseError,
  PowTimeoutError,
  TooLargeError,
  WeakPassphraseError,
  assertStrongPassphrase,
  deriveContent,
  deriveLink,
  formatLink,
  idToString,
  newHeader,
  newRequestKey,
  newSecret,
  requestKey,
  seal,
  utf8,
  type ContentKeys,
  type KdfId,
  type LinkKeys,
  type Meta,
  wipeRequestKey,
  type CryptoRequestKey,
} from '@nylzo/core';

import { ApiError, type Api, type Instance, type InstanceLimits } from './api.ts';
import { sanitizeFileName } from './download.ts';
import { KdfWorkerError } from './kdf-protocol.ts';

export interface FileInput {
  readonly name: string;
  readonly mime: string;
  readonly bytes: Uint8Array<ArrayBuffer>;
}

export interface CreateInput {
  readonly text: string;
  readonly file?: FileInput | undefined;
  readonly ttlSeconds: number;
  readonly opensMax: number;
  readonly passphrase: string;
}

export type CreateProblem =
  | { readonly field: 'content'; readonly code: 'missing' }
  | { readonly field: 'text' | 'file'; readonly code: 'too_large' }
  | { readonly field: 'ttl' | 'opens'; readonly code: 'out_of_range' }
  | { readonly field: 'passphrase'; readonly code: 'too_short' | 'malformed' };

export type CreateField = CreateProblem['field'];

export type CreateProgress =
  | { readonly step: 'preparing' }
  | { readonly step: 'deriving'; readonly fraction?: number | undefined }
  | { readonly step: 'encrypting' }
  | { readonly step: 'proving'; readonly fraction: number }
  | { readonly step: 'uploading' };

export type CreateFailure =
  | { readonly kind: 'invalid'; readonly problems: readonly CreateProblem[] }
  | { readonly kind: 'rate_limited' | 'unavailable'; readonly retryAfter?: number | undefined }
  | { readonly kind: 'too_large' | 'kdf' | 'pow' | 'failed' };

export class CreateError extends Error {
  override readonly name = 'CreateError';
  readonly failure: CreateFailure;

  constructor(failure: CreateFailure) {
    super(`create failed: ${failure.kind}`);
    this.failure = failure;
  }
}

export interface Created {
  readonly link: string;
  readonly expiresAt: string;
  readonly id: string;
  readonly manageKey: CryptoRequestKey;
}

export interface CreateDeps {
  readonly api: Api;
  readonly origin: string;
  readonly scrypt: (
    passphrase: string,
    saltPw: Uint8Array<ArrayBuffer>,
    onProgress: (fraction: number) => void,
  ) => Promise<Uint8Array<ArrayBuffer>>;
  readonly solvePow: (
    challenge: string,
    id: Uint8Array<ArrayBuffer>,
    difficulty: number,
    deadline: number,
    onProgress: (fraction: number) => void,
  ) => Promise<string>;
  readonly now?: () => number;
}

const MAX_ATTEMPTS = 3;
const RETRY_WITH_NEW_ID = new Set(['id_token', 'conflict', 'pow']);
export const WEAK_BELOW_CHARS = 16;
export const WEAK_BELOW_WORDS = 4;
export const POW_BUDGET_MS = 110_000;
export const POW_ROUNDS = 2;

export function passphraseIsWeak(passphrase: string): boolean {
  const normalized = passphrase.normalize('NFC');
  const words = normalized.split(/\s+/).filter(Boolean).length;
  return [...normalized].length < WEAK_BELOW_CHARS || words < WEAK_BELOW_WORDS;
}

export function effectiveKdf(input: Pick<CreateInput, 'passphrase'>): KdfId {
  return input.passphrase === '' ? KDF_NONE : KDF_SCRYPT;
}

export function validateCreate(input: CreateInput, limits: InstanceLimits): CreateProblem[] {
  const problems: CreateProblem[] = [];
  if (input.text === '' && input.file === undefined) {
    problems.push({ field: 'content', code: 'missing' });
  }
  if (utf8(input.text).length > Math.min(limits.maxTextBytes, MAX_TEXT_BYTES)) {
    problems.push({ field: 'text', code: 'too_large' });
  }
  if (input.file && input.file.bytes.length > limits.maxFileBytes) {
    problems.push({ field: 'file', code: 'too_large' });
  }
  if (
    !Number.isInteger(input.ttlSeconds) ||
    input.ttlSeconds < MIN_TTL_SECONDS ||
    input.ttlSeconds > limits.maxTtlSeconds
  ) {
    problems.push({ field: 'ttl', code: 'out_of_range' });
  }
  if (!Number.isInteger(input.opensMax) || input.opensMax < 1 || input.opensMax > limits.maxOpens) {
    problems.push({ field: 'opens', code: 'out_of_range' });
  }
  if (input.passphrase !== '') {
    try {
      assertStrongPassphrase(input.passphrase);
    } catch (error) {
      if (error instanceof WeakPassphraseError) {
        problems.push({ field: 'passphrase', code: 'too_short' });
      } else if (error instanceof MalformedPassphraseError) {
        problems.push({ field: 'passphrase', code: 'malformed' });
      } else {
        throw error;
      }
    }
  }
  return problems;
}

function meta(input: CreateInput): Meta {
  const file = input.file && {
    name: sanitizeFileName(input.file.name),
    mime: utf8(input.file.mime).length <= MAX_MIME ? input.file.mime : '',
    size: input.file.bytes.length,
  };
  return { ...(input.text === '' ? {} : { text: input.text }), ...(file ? { file } : {}) };
}

async function passphraseKey(
  input: CreateInput,
  kdfId: KdfId,
  saltPw: Uint8Array<ArrayBuffer>,
  deps: CreateDeps,
  onProgress: (p: CreateProgress) => void,
) {
  if (kdfId === KDF_NONE) {
    return new Uint8Array(0);
  }
  onProgress({ step: 'deriving', fraction: 0 });
  return deps.scrypt(input.passphrase, saltPw, (fraction) => onProgress({ step: 'deriving', fraction }));
}

async function prove(
  id: Uint8Array<ArrayBuffer>,
  deps: CreateDeps,
  onProgress: (p: CreateProgress) => void,
): Promise<string | undefined> {
  const now = deps.now ?? Date.now;
  for (let round = 1; ; round++) {
    const { pow } = await deps.api.instance();
    if (!pow.required) {
      return undefined;
    }
    onProgress({ step: 'proving', fraction: 0 });
    try {
      return await deps.solvePow(pow.challenge ?? '', id, pow.difficulty, now() + POW_BUDGET_MS, (fraction) =>
        onProgress({ step: 'proving', fraction }),
      );
    } catch (error) {
      if (error instanceof PowTimeoutError && round < POW_ROUNDS) {
        continue;
      }
      throw error instanceof ApiError ? error : new CreateError({ kind: 'pow' });
    }
  }
}

function failureOf(error: unknown): CreateFailure {
  if (error instanceof CreateError) {
    return error.failure;
  }
  if (error instanceof ApiError) {
    if (error.kind === 'rate_limited' || error.kind === 'unavailable') {
      return { kind: error.kind, retryAfter: error.retryAfter };
    }
    if (error.kind === 'too_large') {
      return { kind: 'too_large' };
    }
  }
  if (error instanceof KdfWorkerError) {
    return { kind: 'kdf' };
  }
  if (error instanceof TooLargeError) {
    return { kind: 'too_large' };
  }
  return { kind: 'failed' };
}

interface Sealed {
  readonly object: Uint8Array<ArrayBuffer>;
  readonly readPublicKey: Uint8Array<ArrayBuffer>;
  readonly manageKey: CryptoRequestKey;
  readonly verifierKey: Uint8Array<ArrayBuffer> | undefined;
}

async function sealMessage(
  input: CreateInput,
  instance: Instance,
  kdfId: KdfId,
  s: Uint8Array<ArrayBuffer>,
  deps: CreateDeps,
  onProgress: (p: CreateProgress) => void,
): Promise<Sealed> {
  const { id } = instance.create;
  let link: LinkKeys | undefined;
  let p: Uint8Array | undefined;
  let content: ContentKeys | undefined;
  try {
    link = await deriveLink(id, s);
    p = await passphraseKey(input, kdfId, link.saltPw, deps, onProgress);
    content = await deriveContent(id, s, p);
    p.fill(0);
    const policy = { opensMax: input.opensMax, ttlSeconds: input.ttlSeconds };
    onProgress({ step: 'encrypting' });
    const object = await seal(content.k, newHeader(kdfId), id, policy, { meta: meta(input), file: input.file?.bytes });
    if (object.length > instance.limits.maxObjectBytes) {
      throw new CreateError({ kind: 'too_large' });
    }
    const readKey = await requestKey(link.r);
    wipeRequestKey(readKey);
    const verifier = kdfId === KDF_NONE ? undefined : await requestKey(content.v);
    wipeRequestKey(verifier);
    const manageKey = await newRequestKey();
    return { object, readPublicKey: readKey.publicKey, manageKey, verifierKey: verifier?.publicKey };
  } finally {
    link?.r.fill(0);
    link?.saltPw.fill(0);
    p?.fill(0);
    content?.v.fill(0);
  }
}

async function attempt(
  input: CreateInput,
  deps: CreateDeps,
  onProgress: (p: CreateProgress) => void,
): Promise<Created> {
  onProgress({ step: 'preparing' });
  const instance = await deps.api.instance();
  const problems = validateCreate(input, instance.limits);
  if (problems.length > 0) {
    throw new CreateError({ kind: 'invalid', problems });
  }
  const { id, idToken } = instance.create;
  const kdfId = effectiveKdf(input);
  const s = newSecret();
  try {
    const { object, readPublicKey, manageKey, verifierKey } = await sealMessage(
      input,
      instance,
      kdfId,
      s,
      deps,
      onProgress,
    );
    const powToken = instance.pow.required ? await prove(id, deps, onProgress) : undefined;
    onProgress({ step: 'uploading' });
    const { expiresAt } = await deps.api.create({
      id,
      idToken,
      readKey: readPublicKey,
      ...(verifierKey ? { verifierKey } : {}),
      manageKey: manageKey.publicKey,
      kdfId,
      opensMax: input.opensMax,
      ttlSeconds: input.ttlSeconds,
      object,
      ...(powToken === undefined ? {} : { powToken }),
    });
    return { link: formatLink(deps.origin, id, kdfId, s), expiresAt, id: idToString(id), manageKey };
  } finally {
    s.fill(0);
  }
}

export async function createMessage(
  input: CreateInput,
  deps: CreateDeps,
  onProgress: (progress: CreateProgress) => void = () => {},
): Promise<Created> {
  for (let attemptNo = 1; ; attemptNo++) {
    try {
      return await attempt(input, deps, onProgress);
    } catch (error) {
      if (error instanceof ApiError && RETRY_WITH_NEW_ID.has(error.kind) && attemptNo < MAX_ATTEMPTS) {
        continue;
      }
      throw new CreateError(failureOf(error));
    }
  }
}
