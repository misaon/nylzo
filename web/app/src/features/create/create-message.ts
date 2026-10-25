import {
  assertStrongPassphrase,
  type ContentKeys,
  type CryptoRequestKey,
  deriveContent,
  deriveLink,
  formatLink,
  idToString,
  KDF_NONE,
  KDF_SCRYPT,
  type KdfId,
  type LinkKeys,
  MalformedPassphraseError,
  MAX_MIME,
  MAX_TEXT_BYTES,
  type Meta,
  MIN_TTL_SECONDS,
  newHeader,
  newRequestKey,
  newSecret,
  PowTimeoutError,
  requestKey,
  seal,
  TooLargeError,
  utf8,
  WeakPassphraseError,
  wipeRequestKey,
} from '@nylzo/core';

import type { Api, Instance } from '../../shared/api/api.ts';
import type { InstanceLimits } from '../../shared/api/responses.ts';
import type { Progress } from '../../shared/kdf/kdf.ts';

import { ApiError, isApiError, shouldRetryLater } from '../../shared/api/api-errors.ts';
import { sanitizeFileName } from '../../shared/files/file-name.ts';
import { KdfWorkerError } from '../../shared/kdf/kdf-protocol.ts';

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
  | { readonly field: 'file' | 'text'; readonly code: 'too_large' }
  | { readonly field: 'opens' | 'ttl'; readonly code: 'out_of_range' }
  | { readonly field: 'passphrase'; readonly code: 'malformed' | 'too_short' };

export type CreateField = CreateProblem['field'];

export type CreateProgress =
  | { readonly step: 'deriving'; readonly fraction?: number | undefined }
  | { readonly step: 'encrypting' }
  | { readonly step: 'preparing' }
  | { readonly step: 'proving'; readonly fraction: number }
  | { readonly step: 'uploading' };

export type CreateFailure =
  | { readonly kind: 'failed' | 'kdf' | 'pow' | 'too_large' }
  | { readonly kind: 'invalid'; readonly problems: readonly CreateProblem[] }
  | { readonly kind: 'rate_limited' | 'unavailable'; readonly retryAfter?: number | undefined };

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

export interface CreateDependencies {
  readonly api: Api;
  readonly origin: string;
  readonly scrypt: (
    passphrase: string,
    saltPw: Uint8Array<ArrayBuffer>,
    onProgress: Progress,
  ) => Promise<Uint8Array<ArrayBuffer>>;
  readonly solvePow: (
    challenge: string,
    id: Uint8Array<ArrayBuffer>,
    difficulty: number,
    deadline: number,
    onProgress: Progress,
  ) => Promise<string>;
  readonly now?: () => number;
}

type ReportProgress = (progress: CreateProgress) => void;

interface SealedMessage {
  readonly object: Uint8Array<ArrayBuffer>;
  readonly readPublicKey: Uint8Array<ArrayBuffer>;
  readonly manageKey: CryptoRequestKey;
  readonly verifierKey: Uint8Array<ArrayBuffer> | undefined;
}

const MAX_ATTEMPTS = 3;
const POW_ROUNDS = 2;
const RETRY_WITH_NEW_ID = ['id_token', 'conflict', 'pow'] as const;
export const WEAK_BELOW_CHARS = 16;
export const WEAK_BELOW_WORDS = 4;
export const POW_BUDGET_MS = 110_000;

export function passphraseIsWeak(passphrase: string): boolean {
  const normalized = passphrase.normalize('NFC');
  const words = normalized.split(/\s+/).filter(Boolean).length;
  const characters = normalized.match(/./gsu)?.length ?? 0;
  return characters < WEAK_BELOW_CHARS || words < WEAK_BELOW_WORDS;
}

function passphraseProblem(passphrase: string): CreateProblem | undefined {
  try {
    assertStrongPassphrase(passphrase);
    return undefined;
  } catch (error) {
    if (error instanceof WeakPassphraseError) {
      return { field: 'passphrase', code: 'too_short' };
    }
    if (error instanceof MalformedPassphraseError) {
      return { field: 'passphrase', code: 'malformed' };
    }
    throw error;
  }
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
  const { ttlSeconds, opensMax } = input;
  if (!Number.isSafeInteger(ttlSeconds) || ttlSeconds < MIN_TTL_SECONDS || ttlSeconds > limits.maxTtlSeconds) {
    problems.push({ field: 'ttl', code: 'out_of_range' });
  }
  if (!Number.isSafeInteger(opensMax) || opensMax < 1 || opensMax > limits.maxOpens) {
    problems.push({ field: 'opens', code: 'out_of_range' });
  }
  const passphrase = input.passphrase === '' ? undefined : passphraseProblem(input.passphrase);
  if (passphrase) {
    problems.push(passphrase);
  }
  return problems;
}

function metaOf(input: CreateInput): Meta {
  const file = input.file && {
    name: sanitizeFileName(input.file.name),
    mime: utf8(input.file.mime).length <= MAX_MIME ? input.file.mime : '',
    size: input.file.bytes.length,
  };
  return { ...(input.text !== '' && { text: input.text }), ...(file && { file }) };
}

async function derivePassphraseKey(
  input: CreateInput,
  kdfId: KdfId,
  saltPw: Uint8Array<ArrayBuffer>,
  dependencies: CreateDependencies,
  onProgress: ReportProgress,
): Promise<Uint8Array> {
  if (kdfId === KDF_NONE) {
    return new Uint8Array(0);
  }
  onProgress({ step: 'deriving', fraction: 0 });
  return dependencies.scrypt(input.passphrase, saltPw, (fraction) => onProgress({ step: 'deriving', fraction }));
}

async function prove(
  id: Uint8Array<ArrayBuffer>,
  dependencies: CreateDependencies,
  onProgress: ReportProgress,
): Promise<string | undefined> {
  const now = dependencies.now ?? Date.now;
  for (let round = 1; ; round++) {
    const { pow } = await dependencies.api.instance();
    if (!pow.required) {
      return undefined;
    }
    onProgress({ step: 'proving', fraction: 0 });
    try {
      const deadline = now() + POW_BUDGET_MS;
      return await dependencies.solvePow(pow.challenge ?? '', id, pow.difficulty, deadline, (fraction) =>
        onProgress({ step: 'proving', fraction }),
      );
    } catch (error) {
      if (!(error instanceof PowTimeoutError) || round >= POW_ROUNDS) {
        throw error instanceof ApiError ? error : new CreateError({ kind: 'pow' });
      }
    }
  }
}

function failureOf(error: unknown): CreateFailure {
  if (error instanceof CreateError) {
    return error.failure;
  }
  if (shouldRetryLater(error)) {
    return { kind: error.kind === 'rate_limited' ? 'rate_limited' : 'unavailable', retryAfter: error.retryAfter };
  }
  if (isApiError(error, 'too_large') || error instanceof TooLargeError) {
    return { kind: 'too_large' };
  }
  return error instanceof KdfWorkerError ? { kind: 'kdf' } : { kind: 'failed' };
}

async function sealMessage(
  input: CreateInput,
  instance: Instance,
  kdfId: KdfId,
  secret: Uint8Array<ArrayBuffer>,
  dependencies: CreateDependencies,
  onProgress: ReportProgress,
): Promise<SealedMessage> {
  const { id } = instance.create;
  let linkKeys: LinkKeys | undefined;
  let passphraseKey: Uint8Array | undefined;
  let contentKeys: ContentKeys | undefined;
  try {
    linkKeys = await deriveLink(id, secret);
    passphraseKey = await derivePassphraseKey(input, kdfId, linkKeys.saltPw, dependencies, onProgress);
    contentKeys = await deriveContent(id, secret, passphraseKey);
    passphraseKey.fill(0);
    const policy = { opensMax: input.opensMax, ttlSeconds: input.ttlSeconds };
    onProgress({ step: 'encrypting' });
    const object = await seal(contentKeys.k, newHeader(kdfId), id, policy, {
      meta: metaOf(input),
      file: input.file?.bytes,
    });
    if (object.length > instance.limits.maxObjectBytes) {
      throw new CreateError({ kind: 'too_large' });
    }
    const readKey = await requestKey(linkKeys.r);
    wipeRequestKey(readKey);
    const verifierKey = kdfId === KDF_NONE ? undefined : await requestKey(contentKeys.v);
    wipeRequestKey(verifierKey);
    const manageKey = await newRequestKey();
    return { object, readPublicKey: readKey.publicKey, manageKey, verifierKey: verifierKey?.publicKey };
  } finally {
    linkKeys?.r.fill(0);
    linkKeys?.saltPw.fill(0);
    passphraseKey?.fill(0);
    contentKeys?.v.fill(0);
  }
}

async function createOnce(
  input: CreateInput,
  dependencies: CreateDependencies,
  onProgress: ReportProgress,
): Promise<Created> {
  onProgress({ step: 'preparing' });
  const instance = await dependencies.api.instance();
  const problems = validateCreate(input, instance.limits);
  if (problems.length > 0) {
    throw new CreateError({ kind: 'invalid', problems });
  }
  const { id, idToken } = instance.create;
  const kdfId = input.passphrase === '' ? KDF_NONE : KDF_SCRYPT;
  const secret = newSecret();
  try {
    const sealed = await sealMessage(input, instance, kdfId, secret, dependencies, onProgress);
    const powToken = instance.pow.required ? await prove(id, dependencies, onProgress) : undefined;
    onProgress({ step: 'uploading' });
    const { expiresAt } = await dependencies.api.create({
      id,
      idToken,
      readKey: sealed.readPublicKey,
      ...(sealed.verifierKey && { verifierKey: sealed.verifierKey }),
      manageKey: sealed.manageKey.publicKey,
      kdfId,
      opensMax: input.opensMax,
      ttlSeconds: input.ttlSeconds,
      object: sealed.object,
      ...(powToken !== undefined && { powToken }),
    });
    const link = formatLink(dependencies.origin, id, kdfId, secret);
    return { link, expiresAt, id: idToString(id), manageKey: sealed.manageKey };
  } finally {
    secret.fill(0);
  }
}

export async function createMessage(
  input: CreateInput,
  dependencies: CreateDependencies,
  onProgress?: ReportProgress,
): Promise<Created> {
  const reportProgress: ReportProgress = (progress) => onProgress?.(progress);
  for (let attempt = 1; ; attempt++) {
    try {
      return await createOnce(input, dependencies, reportProgress);
    } catch (error) {
      if (!isApiError(error, ...RETRY_WITH_NEW_ID) || attempt >= MAX_ATTEMPTS) {
        throw new CreateError(failureOf(error));
      }
    }
  }
}
