import { utf8 } from './bytes.ts';
import { MetaError } from './errors.ts';
import { isIntegerLiteral, parseJson, type JsonValue } from './json.ts';
import { MAX_FILE_BYTES, MAX_FILE_NAME, MAX_META_BYTES, MAX_MIME, MAX_TEXT_BYTES } from './limits.ts';

export interface FileMeta {
  readonly name: string;
  readonly mime: string;
  readonly size: number;
}

export interface Meta {
  readonly text?: string | undefined;
  readonly file?: FileMeta | undefined;
}

const FORBIDDEN_IN_FILE_NAME = /[\u0000-\u001f\u007f/\\]/;
const decoder = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });

export function hasFile(meta: Meta): boolean {
  return meta.file !== undefined;
}

export function validateMeta(meta: Meta): void {
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
  validateMeta(meta);
  const bytes = utf8(metaJson(meta));
  if (bytes.length > MAX_META_BYTES) {
    throw new MetaError('meta exceeds the size limit');
  }
  return bytes;
}

export function decodeMeta(bytes: Uint8Array): Meta {
  if (bytes.length > MAX_META_BYTES) {
    throw new MetaError('meta exceeds the size limit');
  }
  const meta = metaFromJson(parseMetaJson(bytes));
  validateMeta(meta);
  return meta;
}

function validateText(text: string): void {
  if (!text.isWellFormed() || utf8(text).length > MAX_TEXT_BYTES) {
    throw new MetaError('text is not valid UTF-8 within the limit');
  }
}

function validateFile(file: FileMeta): void {
  validateFileName(file.name);
  if (!file.mime.isWellFormed() || utf8(file.mime).length > MAX_MIME) {
    throw new MetaError('file.mime is not valid UTF-8 within the limit');
  }
  if (!Number.isInteger(file.size) || file.size < 0 || file.size > MAX_FILE_BYTES) {
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

function metaJson(meta: Meta): string {
  let json = '{"v":1';
  if (meta.text !== undefined) {
    json += `,"text":${JSON.stringify(meta.text)}`;
  }
  if (meta.file !== undefined) {
    const { name, mime, size } = meta.file;
    json += `,"file":{"name":${JSON.stringify(name)},"mime":${JSON.stringify(mime)},"size":${String(size)}}`;
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

function metaFromJson(value: JsonValue): Meta {
  const members = objectMembers(value);
  if (requireInteger(member(members, 'v')) !== 1) {
    throw new MetaError('meta.v must be 1');
  }
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
  if (value === undefined || value.kind !== 'number' || !isIntegerLiteral(value.literal)) {
    throw new MetaError('expected a JSON integer');
  }
  const n = Number(value.literal);
  if (!Number.isSafeInteger(n)) {
    throw new MetaError('integer is out of range');
  }
  return n === 0 ? 0 : n;
}
