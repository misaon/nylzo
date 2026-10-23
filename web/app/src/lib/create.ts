import {
  KDF_NONE,
  KDF_PBKDF2,
  KDF_SCRYPT,
  MAX_MIME,
  MAX_TEXT_BYTES,
  MIN_TTL_SECONDS,
  MalformedPassphraseError,
  WeakPassphraseError,
  assertStrongPassphrase,
  deriveContent,
  deriveLink,
  derivePassphraseKey,
  formatLink,
  newHeader,
  newSecret,
  requestKey,
  seal,
  utf8,
  type KdfId,
  type Meta,
} from '@nylzo/core';

import { ApiError, type Api, type InstanceLimits } from './api.ts';
import { sanitizeFileName } from './download.ts';

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
  readonly kdfId: KdfId;
}

export type CreateField = 'content' | 'text' | 'file' | 'ttl' | 'opens' | 'passphrase';

export interface CreateProblem {
  readonly field: CreateField;
  readonly code: 'missing' | 'too_large' | 'out_of_range' | 'too_short' | 'malformed';
}

export type CreateProgress =
  | { readonly step: 'preparing' }
  | { readonly step: 'deriving'; readonly fraction: number }
  | { readonly step: 'proving'; readonly fraction: number }
  | { readonly step: 'uploading' };

export type CreateFailure =
  | { readonly kind: 'invalid'; readonly problems: readonly CreateProblem[] }
  | { readonly kind: 'rate_limited' | 'unavailable'; readonly retryAfter?: number | undefined }
  | { readonly kind: 'too_large' | 'kdf' | 'failed' };

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
}

export interface CreateDeps {
  readonly api: Api;
  readonly origin: string;
  readonly scrypt: (passphrase: string, saltPw: Uint8Array<ArrayBuffer>, onProgress: (fraction: number) => void) => Promise<Uint8Array<ArrayBuffer>>;
  readonly solvePow: (challenge: string, id: Uint8Array<ArrayBuffer>, difficulty: number, onProgress: (fraction: number) => void) => Promise<string>;
}

const MAX_ATTEMPTS = 3;
const RETRY_WITH_NEW_ID = new Set(['id_token', 'conflict', 'pow']);
const WEAK_BELOW_CHARS = 16;
const WEAK_BELOW_WORDS = 4;

export function passphraseIsWeak(passphrase: string): boolean {
  const normalized = passphrase.normalize('NFC');
  const words = normalized.split(/\s+/).filter(Boolean).length;
  return [...normalized].length < WEAK_BELOW_CHARS || words < WEAK_BELOW_WORDS;
}

export function effectiveKdf(input: Pick<CreateInput, 'passphrase' | 'kdfId'>): KdfId {
  return input.passphrase === '' ? KDF_NONE : input.kdfId;
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
  if (!Number.isInteger(input.ttlSeconds) || input.ttlSeconds < MIN_TTL_SECONDS || input.ttlSeconds > limits.maxTtlSeconds) {
    problems.push({ field: 'ttl', code: 'out_of_range' });
  }
  if (!Number.isInteger(input.opensMax) || input.opensMax < 1 || input.opensMax > limits.maxOpens) {
    problems.push({ field: 'opens', code: 'out_of_range' });
  }
  if (input.passphrase !== '') {
    try {
      assertStrongPassphrase(input.passphrase);
      if (input.kdfId !== KDF_SCRYPT && input.kdfId !== KDF_PBKDF2) {
        problems.push({ field: 'passphrase', code: 'out_of_range' });
      }
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

async function passphraseKey(input: CreateInput, kdfId: KdfId, saltPw: Uint8Array<ArrayBuffer>, deps: CreateDeps, onProgress: (p: CreateProgress) => void) {
  if (kdfId === KDF_SCRYPT) {
    onProgress({ step: 'deriving', fraction: 0 });
    return deps.scrypt(input.passphrase, saltPw, (fraction) => onProgress({ step: 'deriving', fraction }));
  }
  if (kdfId === KDF_PBKDF2) {
    onProgress({ step: 'deriving', fraction: 0 });
  }
  return derivePassphraseKey(kdfId, input.passphrase, saltPw);
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
  if (error instanceof Error && error.name === 'KdfWorkerError') {
    return { kind: 'kdf' };
  }
  if (error instanceof Error && error.name === 'TooLargeError') {
    return { kind: 'too_large' };
  }
  return { kind: 'failed' };
}

async function attempt(input: CreateInput, deps: CreateDeps, onProgress: (p: CreateProgress) => void): Promise<Created> {
  onProgress({ step: 'preparing' });
  const instance = await deps.api.instance();
  const problems = validateCreate(input, instance.limits);
  if (problems.length > 0) {
    throw new CreateError({ kind: 'invalid', problems });
  }
  const { id, idToken } = instance.create;
  const kdfId = effectiveKdf(input);
  const s = newSecret();
  const m = newSecret();
  const link = await deriveLink(id, s);
  const p = await passphraseKey(input, kdfId, link.saltPw, deps, onProgress);
  const content = await deriveContent(id, s, p);
  const policy = { opensMax: input.opensMax, ttlSeconds: input.ttlSeconds };
  const object = await seal(content.k, newHeader(kdfId), id, policy, { meta: meta(input), file: input.file?.bytes });
  if (object.length > instance.limits.maxObjectBytes) {
    throw new CreateError({ kind: 'too_large' });
  }
  const [readKey, manageKey] = await Promise.all([requestKey(link.r), requestKey(m)]);
  const verifierKey = kdfId === KDF_NONE ? undefined : (await requestKey(content.v)).publicKey;
  let powToken: string | undefined;
  if (instance.pow.required) {
    onProgress({ step: 'proving', fraction: 0 });
    powToken = await deps.solvePow(instance.pow.challenge ?? '', id, instance.pow.difficulty, (fraction) =>
      onProgress({ step: 'proving', fraction }),
    );
  }
  onProgress({ step: 'uploading' });
  const { expiresAt } = await deps.api.create({
    id,
    idToken,
    readKey: readKey.publicKey,
    ...(verifierKey ? { verifierKey } : {}),
    manageKey: manageKey.publicKey,
    kdfId,
    opensMax: input.opensMax,
    ttlSeconds: input.ttlSeconds,
    object,
    ...(powToken === undefined ? {} : { powToken }),
  });
  return { link: formatLink(deps.origin, id, kdfId, s), expiresAt };
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
