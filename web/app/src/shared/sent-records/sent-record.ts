import { type CryptoRequestKey, parseId, PUBLIC_KEY_LEN } from '@nylzo/core';

const DAY_MS = 24 * 60 * 60 * 1000;
export const RETENTION_DAYS = 7;
export const RETENTION_MS = RETENTION_DAYS * DAY_MS;
const MAX_LIFETIME_MS = 31 * DAY_MS;
const MAX_TIME_MS = 8.64e15;

export interface SentRecord {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly endedAt?: number | undefined;
  readonly manageKey: CryptoRequestKey;
}

export interface StoredRecord {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly endedAt?: number;
  readonly privateKey: CryptoKey;
  readonly publicKey: Uint8Array<ArrayBuffer>;
}

export class ExtractableKeyError extends Error {
  override readonly name = 'ExtractableKeyError';

  constructor() {
    super('a manage key must not be extractable');
  }
}

export class InvalidRecordError extends Error {
  override readonly name = 'InvalidRecordError';

  constructor() {
    super('a sent record has impossible times or keys');
  }
}

function isMessageId(id: unknown): id is string {
  if (typeof id !== 'string') {
    return false;
  }
  try {
    parseId(id);
    return true;
  } catch {
    return false;
  }
}

function isTime(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 && value <= MAX_TIME_MS;
}

function areTimes(createdAt: unknown, expiresAt: unknown, endedAt: unknown): boolean {
  return (
    isTime(createdAt) &&
    isTime(expiresAt) &&
    createdAt <= expiresAt &&
    expiresAt - createdAt <= MAX_LIFETIME_MS &&
    (endedAt === undefined || (isTime(endedAt) && endedAt <= expiresAt + MAX_LIFETIME_MS))
  );
}

function isSigningKey(key: unknown): key is CryptoKey {
  return (
    key instanceof CryptoKey &&
    key.type === 'private' &&
    key.algorithm.name === 'Ed25519' &&
    key.usages.includes('sign')
  );
}

export function storedForm(record: SentRecord): StoredRecord {
  if (record.manageKey.privateKey.extractable) {
    throw new ExtractableKeyError();
  }
  if (!isMessageId(record.id) || !areTimes(record.createdAt, record.expiresAt, record.endedAt)) {
    throw new InvalidRecordError();
  }
  return {
    id: record.id,
    createdAt: record.createdAt,
    expiresAt: record.expiresAt,
    ...(record.endedAt !== undefined && { endedAt: record.endedAt }),
    privateKey: record.manageKey.privateKey,
    publicKey: record.manageKey.publicKey,
  };
}

export function recordFrom(value: unknown): SentRecord | undefined {
  if (typeof value !== 'object' || value === null) {
    return undefined;
  }
  const { id, createdAt, expiresAt, endedAt, privateKey, publicKey } = value as Partial<
    Record<keyof StoredRecord, unknown>
  >;
  if (
    !isMessageId(id) ||
    !areTimes(createdAt, expiresAt, endedAt) ||
    !isTime(createdAt) ||
    !isTime(expiresAt) ||
    !isSigningKey(privateKey) ||
    privateKey.extractable ||
    !(publicKey instanceof Uint8Array) ||
    publicKey.length !== PUBLIC_KEY_LEN
  ) {
    return undefined;
  }
  return {
    id,
    createdAt,
    expiresAt,
    ...(isTime(endedAt) && { endedAt }),
    manageKey: { privateKey, publicKey: new Uint8Array(publicKey) },
  };
}

export function isRetained(record: Pick<SentRecord, 'endedAt' | 'expiresAt'>, now: number): boolean {
  return now < Math.min(record.expiresAt, record.endedAt ?? Infinity) + RETENTION_MS;
}
