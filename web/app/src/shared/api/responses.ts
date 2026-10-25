import { encodeBase64url, utf8 } from '@nylzo/core';

import { ApiError, ERROR_KINDS, MESSAGE_STATES, type MessageState, ResponseError } from './api-errors.ts';

export type Json = Record<string, unknown>;

export interface InstanceLimits {
  readonly maxFileBytes: number;
  readonly maxTextBytes: number;
  readonly maxObjectBytes: number;
  readonly maxTtlSeconds: number;
  readonly defaultTtlSeconds: number;
  readonly maxOpens: number;
}

export interface Operator {
  readonly name?: string;
  readonly contact?: string;
  readonly hosting?: string;
}

export interface ClientFile {
  readonly path: string;
  readonly sha256: string;
}

export interface MessageTimes {
  readonly createdAt?: string;
  readonly expiresAt?: string;
  readonly firstOpenedAt?: string;
  readonly endedAt?: string;
}

export interface ServedPolicy {
  readonly opensMax: number;
  readonly ttlSeconds: number;
  readonly kdfId: number;
  readonly flags: number;
}

const MAX_OPERATOR_FIELD_BYTES = 200;
const SHA256_HEX = /^[\da-f]{64}$/;
const RFC_3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const BASE64URL = /^[\w-]+$/;
const TIMES = [
  ['created_at', 'createdAt'],
  ['expires_at', 'expiresAt'],
  ['first_opened_at', 'firstOpenedAt'],
  ['ended_at', 'endedAt'],
] as const;

export function isObject(value: unknown): value is Json {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function integer(json: Json, key: string): number {
  const value = json[key];
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) {
    throw new ResponseError(`field ${key} is not a non-negative integer`);
  }
  return value;
}

export function text(json: Json, key: string): string {
  const value = json[key];
  if (typeof value !== 'string') {
    throw new ResponseError(`field ${key} is not a string`);
  }
  return value;
}

export function timestamp(json: Json, key: string): string {
  const value = text(json, key);
  if (!RFC_3339.test(value) || !Number.isFinite(Date.parse(value))) {
    throw new ResponseError(`field ${key} is not an RFC 3339 time`);
  }
  return value;
}

export function messageState(json: Json): MessageState {
  const value = json['state'];
  const found = MESSAGE_STATES.find((state) => state === value);
  if (!found) {
    throw new ResponseError('unknown message state');
  }
  return found;
}

export function messageTimes(json: Json): MessageTimes {
  const times: { -readonly [Key in keyof MessageTimes]: MessageTimes[Key] } = {};
  for (const [wire, field] of TIMES) {
    if (json[wire] !== undefined) {
      times[field] = timestamp(json, wire);
    }
  }
  return times;
}

export function instanceLimits(limits: Json): InstanceLimits {
  return {
    maxFileBytes: integer(limits, 'max_file_bytes'),
    maxTextBytes: integer(limits, 'max_text_bytes'),
    maxObjectBytes: integer(limits, 'max_object_bytes'),
    maxTtlSeconds: integer(limits, 'max_ttl_s'),
    defaultTtlSeconds: integer(limits, 'default_ttl_s'),
    maxOpens: integer(limits, 'max_opens'),
  };
}

function operatorField(operator: Json, key: string): string | undefined {
  const value = operator[key];
  if (value === undefined) {
    return undefined;
  }
  if (typeof value !== 'string' || value === '' || utf8(value).length > MAX_OPERATOR_FIELD_BYTES) {
    throw new ResponseError();
  }
  return value;
}

export function operatorOf(value: unknown): Operator | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (!isObject(value)) {
    throw new ResponseError();
  }
  const operator: { -readonly [Key in keyof Operator]: Operator[Key] } = {};
  for (const key of ['name', 'contact', 'hosting'] as const) {
    const field = operatorField(value, key);
    if (field !== undefined) {
      operator[key] = field;
    }
  }
  return operator;
}

export function clientFiles(manifest: Json): readonly ClientFile[] {
  const files = manifest['files'];
  if (!Array.isArray(files) || typeof manifest['built'] !== 'boolean') {
    throw new ResponseError();
  }
  const seen = new Set<string>();
  return files.map((file: unknown) => {
    if (!isObject(file)) {
      throw new ResponseError();
    }
    const path = text(file, 'path');
    const sha256 = text(file, 'sha256');
    if (!path.startsWith('/') || !SHA256_HEX.test(sha256) || seen.has(path)) {
      throw new ResponseError();
    }
    seen.add(path);
    return { path, sha256 };
  });
}

export async function readJson(response: Response): Promise<Json> {
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    throw new ResponseError();
  }
  if (!isObject(body)) {
    throw new ResponseError();
  }
  return body;
}

function retryAfterHeader(response: Response): number | undefined {
  const header = response.headers.get('Retry-After');
  const value = header === null || header.trim() === '' ? NaN : Number(header);
  return Number.isSafeInteger(value) && value >= 0 ? value : undefined;
}

export async function apiError(response: Response): Promise<ApiError> {
  let body: Json;
  try {
    body = await readJson(response);
  } catch {
    return new ApiError(response.status, 'unexpected', { retryAfter: retryAfterHeader(response) });
  }
  const kind = ERROR_KINDS.find((candidate) => candidate === body['error']) ?? 'unexpected';
  return new ApiError(response.status, kind, {
    retryAfter: typeof body['retry_after'] === 'number' ? integer(body, 'retry_after') : retryAfterHeader(response),
    attemptsLeft: typeof body['attempts_left'] === 'number' ? integer(body, 'attempts_left') : undefined,
    state: typeof body['state'] === 'string' ? messageState(body) : undefined,
  });
}

export function servedPolicy(header: string | null): ServedPolicy {
  if (header === null || !BASE64URL.test(header)) {
    throw new ResponseError('missing or malformed X-Nylzo-Policy');
  }
  const standard = header.replaceAll('-', '+').replaceAll('_', '/');
  const padded = standard.padEnd(Math.ceil(standard.length / 4) * 4, '=');
  const bytes = Uint8Array.from(atob(padded), (character) => character.codePointAt(0) ?? 0);
  if (encodeBase64url(bytes) !== header) {
    throw new ResponseError('X-Nylzo-Policy is not canonical base64url');
  }
  let json: unknown;
  try {
    json = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
  } catch {
    throw new ResponseError('X-Nylzo-Policy is not JSON');
  }
  if (!isObject(json)) {
    throw new ResponseError('X-Nylzo-Policy is not an object');
  }
  return {
    opensMax: integer(json, 'opens_max'),
    ttlSeconds: integer(json, 'ttl_s'),
    kdfId: integer(json, 'kdf_id'),
    flags: integer(json, 'flags'),
  };
}
