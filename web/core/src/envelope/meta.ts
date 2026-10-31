import { MetaError } from '../errors.ts';
import {
  isUnsignedInteger,
  MAX_FILE_BYTES,
  MAX_FILE_NAME,
  MAX_META_BYTES,
  MAX_MIME_BYTES,
  MAX_TEXT_BYTES,
} from '../limits.ts';
import { utf8 } from '../primitives/bytes.ts';
import { isIntegerLiteral, type JsonValue, parseJson } from './json.ts';

interface FileMeta {
  readonly name: string;
  readonly mime: string;
  readonly size: number;
}

export interface Meta {
  readonly text?: string | undefined;
  readonly file?: FileMeta | undefined;
}

// eslint-disable-next-line no-control-regex -- spec 3.1 forbids C0 controls and DEL in file names
const FORBIDDEN_IN_FILE_NAME = /[\u{0}-\u{1F}\u{7F}/\\]/u;
const decoder = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });

function validateMeta(meta: Meta): void {
  if (meta.text === undefined && meta.file === undefined) {
    throw new MetaError('meta needs text or file');
  }
  if (meta.text !== undefined) {
    validateText(meta.text);
  }
  if (meta.file !== undefined) {
    validateFile(meta.file);
  }
}

export function encodeMeta(meta: Meta): Uint8Array<ArrayBuffer> {
  return encodeStoredMeta(meta);
}

export function encodeStoredMeta(meta: Meta, compressedSize?: number): Uint8Array<ArrayBuffer> {
  validateMeta(meta);
  const bytes = utf8(canonicalJson(meta, compressedSize));
  if (bytes.length > MAX_META_BYTES) {
    throw new MetaError('meta exceeds the size limit');
  }
  return bytes;
}

export function decodeMeta(bytes: Uint8Array): Meta {
  const stored = decodeStoredMeta(bytes);
  if (stored.compressedSize !== undefined) {
    throw new MetaError('compressed metadata needs an attachment decoder');
  }
  return stored.meta;
}

export interface StoredMeta {
  readonly meta: Meta;
  readonly compressedSize?: number | undefined;
}

export function decodeStoredMeta(bytes: Uint8Array): StoredMeta {
  if (bytes.length > MAX_META_BYTES) {
    throw new MetaError('meta exceeds the size limit');
  }
  const members = objectMembers(parseMetaJson(bytes));
  const version = requireInteger(member(members, 'v'));
  if (version !== 1 && version !== 2) {
    throw new MetaError('unsupported metadata version');
  }
  const meta = metaFromJson(members);
  validateMeta(meta);
  if (version === 1) {
    return { meta };
  }
  const file = member(members, 'file');
  if (file === undefined || meta.file === undefined) {
    throw new MetaError('compressed metadata needs a file');
  }
  const fileMembers = objectMembers(file);
  const compressedSize = requireInteger(member(fileMembers, 'encoded_size'));
  if (
    optionalString(member(fileMembers, 'encoding')) !== 'gzip' ||
    compressedSize <= 0 ||
    compressedSize >= meta.file.size
  ) {
    throw new MetaError('invalid file encoding');
  }
  return { meta, compressedSize };
}

function validateText(text: string): void {
  if (!text.isWellFormed() || utf8(text).length > MAX_TEXT_BYTES) {
    throw new MetaError('text is not valid UTF-8 within the limit');
  }
}

function validateFile(file: FileMeta): void {
  validateFileName(file.name);
  if (!file.mime.isWellFormed() || utf8(file.mime).length > MAX_MIME_BYTES) {
    throw new MetaError('file.mime is not valid UTF-8 within the limit');
  }
  if (!isUnsignedInteger(file.size, MAX_FILE_BYTES)) {
    throw new MetaError('file.size is out of range');
  }
}

function validateFileName(name: string): void {
  if (name.length === 0 || !name.isWellFormed() || utf8(name).length > MAX_FILE_NAME) {
    throw new MetaError('file.name is not valid UTF-8 within the limit');
  }
  if (FORBIDDEN_IN_FILE_NAME.test(name)) {
    throw new MetaError('file.name contains a forbidden character');
  }
}

function canonicalJson(meta: Meta, compressedSize?: number): string {
  let json = compressedSize === undefined ? '{"v":1' : '{"v":2';
  if (meta.text !== undefined) {
    json += `,"text":${JSON.stringify(meta.text)}`;
  }
  if (meta.file !== undefined) {
    const { name, mime, size } = meta.file;
    json += `,"file":{"name":${JSON.stringify(name)},"mime":${JSON.stringify(mime)},"size":${String(size)}`;
    if (compressedSize !== undefined) {
      json += `,"encoding":"gzip","encoded_size":${String(compressedSize)}`;
    }
    json += '}';
  }
  return `${json}}`;
}

function parseMetaJson(bytes: Uint8Array): JsonValue {
  try {
    return parseJson(decoder.decode(bytes));
  } catch {
    throw new MetaError('meta is not a valid UTF-8 JSON document');
  }
}

function metaFromJson(members: ReadonlyMap<string, JsonValue>): Meta {
  const file = member(members, 'file');
  return {
    text: optionalString(member(members, 'text')),
    file: file === undefined ? undefined : fileFromJson(file),
  };
}

function fileFromJson(value: JsonValue): FileMeta {
  const members = objectMembers(value);
  const name = optionalString(member(members, 'name'));
  if (name === undefined) {
    throw new MetaError('file.name is required');
  }
  return {
    name,
    mime: optionalString(member(members, 'mime')) ?? '',
    size: requireInteger(member(members, 'size')),
  };
}

function objectMembers(value: JsonValue): ReadonlyMap<string, JsonValue> {
  if (value.kind !== 'object') {
    throw new MetaError('expected a JSON object');
  }
  return value.members;
}

function member(members: ReadonlyMap<string, JsonValue>, key: string): JsonValue | undefined {
  const value = members.get(key);
  return value === undefined || value.kind === 'null' ? undefined : value;
}

function optionalString(value: JsonValue | undefined): string | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (value.kind !== 'string') {
    throw new MetaError('expected a JSON string');
  }
  return value.value;
}

function requireInteger(value: JsonValue | undefined): number {
  if (value?.kind !== 'number' || !isIntegerLiteral(value.literal)) {
    throw new MetaError('expected a JSON integer');
  }
  const integer = Number(value.literal);
  if (!Number.isSafeInteger(integer)) {
    throw new MetaError('integer is out of range');
  }
  return integer === 0 ? 0 : integer;
}
