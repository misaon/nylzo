import { parseId, PUBLIC_KEY_LENGTH } from '@nylzo/core';
import type { RequestKey } from '@nylzo/core';

const DAY_MS = 24 * 60 * 60 * 1000;
export const RETENTION_DAYS = 1;
export const RETENTION_MS = RETENTION_DAYS * DAY_MS;
const MAX_LIFETIME_MS = 31 * DAY_MS;
const MAX_TIME_MS = 8.64e15;
export const MAX_LABEL_CHARACTERS = 80;

export interface SentRecord {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly label?: string | undefined;
  readonly manageKey: RequestKey;
}

export interface StoredRecord {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly label?: string;
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

function areTimes(createdAt: unknown, expiresAt: unknown): boolean {
  return isTime(createdAt) && isTime(expiresAt) && createdAt <= expiresAt && expiresAt - createdAt <= MAX_LIFETIME_MS;
}

function isLabel(label: unknown): label is string {
  return (
    typeof label === 'string' &&
    label !== '' &&
    label.isWellFormed() &&
    label.trim() === label &&
    (label.match(/./gsu)?.length ?? 0) <= MAX_LABEL_CHARACTERS
  );
}

function isSigningKey(key: unknown): key is CryptoKey {
  return (
    key instanceof CryptoKey &&
    key.type === 'private' &&
    key.algorithm.name === 'Ed25519' &&
    key.usages.length === 1 &&
    key.usages[0] === 'sign'
  );
}

export function storedForm(record: SentRecord): StoredRecord {
  if (record.manageKey.privateKey.extractable) {
    throw new ExtractableKeyError();
  }
  if (
    !isMessageId(record.id) ||
    !areTimes(record.createdAt, record.expiresAt) ||
    (record.label !== undefined && record.label !== '' && !isLabel(record.label)) ||
    !isSigningKey(record.manageKey.privateKey) ||
    !(record.manageKey.publicKey instanceof Uint8Array) ||
    record.manageKey.publicKey.length !== PUBLIC_KEY_LENGTH
  ) {
    throw new InvalidRecordError();
  }
  return {
    id: record.id,
    createdAt: record.createdAt,
    expiresAt: record.expiresAt,
    ...(isLabel(record.label) && { label: record.label }),
    privateKey: record.manageKey.privateKey,
    publicKey: record.manageKey.publicKey,
  };
}

export function recordFrom(value: unknown): SentRecord | undefined {
  if (typeof value !== 'object' || value === null) {
    return undefined;
  }
  const { id, createdAt, expiresAt, label, privateKey, publicKey } = value as Partial<
    Record<keyof StoredRecord, unknown>
  >;
  // Older clients added endedAt once the message ended, so such a record has nothing left to manage.
  if (
    'endedAt' in value ||
    !isMessageId(id) ||
    !isTime(createdAt) ||
    !isTime(expiresAt) ||
    !areTimes(createdAt, expiresAt) ||
    !isSigningKey(privateKey) ||
    privateKey.extractable ||
    !(publicKey instanceof Uint8Array) ||
    publicKey.length !== PUBLIC_KEY_LENGTH
  ) {
    return undefined;
  }
  return {
    id,
    createdAt,
    expiresAt,
    ...(isLabel(label) && { label }),
    manageKey: { privateKey, publicKey: new Uint8Array(publicKey) },
  };
}

export function retainedUntil(record: Pick<SentRecord, 'expiresAt'>): number {
  return record.expiresAt + RETENTION_MS;
}

export function isRetained(record: Pick<SentRecord, 'expiresAt'>, now: number): boolean {
  return now < retainedUntil(record);
}
