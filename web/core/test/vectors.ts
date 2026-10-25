import { readFileSync } from 'node:fs';

import { concatenate, hash, ID_LEN } from '../src/index.ts';

interface ScryptVector {
  readonly name: string;
  readonly password: string;
  readonly salt: string;
  readonly N: number;
  readonly r: number;
  readonly p: number;
  readonly dkLen: number;
  readonly dk_hex: string;
  readonly heavy: boolean;
}

interface DeriveVector {
  readonly name: string;
  readonly id: string;
  readonly s: string;
  readonly kdf_id: number;
  readonly passphrase?: string;
  readonly hsalt_hex: string;
  readonly r_hex: string;
  readonly salt_pw_hex: string;
  readonly p_hex: string;
  readonly k_hex: string;
  readonly v_hex: string;
  readonly m_hex: string;
  readonly fragment: string;
  readonly read_key: string;
  readonly verifier_key?: string;
  readonly manage_key: string;
}

interface RequestVector {
  readonly name: string;
  readonly seed_hex: string;
  readonly public_key: string;
  readonly purpose: 'read' | 'verifier' | 'manage';
  readonly method: string;
  readonly path: string;
  readonly challenge: string;
  readonly body_hex: string;
  readonly message_hex: string;
  readonly signature: string;
  readonly authorization?: string;
  readonly verifier_header?: string;
}

interface FileVector {
  readonly name: string;
  readonly mime: string;
  readonly size: number;
  readonly content: string;
}

interface EnvelopeVector {
  readonly name: string;
  readonly id: string;
  readonly s: string;
  readonly kdf_id: number;
  readonly passphrase?: string;
  readonly flags: number;
  readonly iv_hex: string;
  readonly opens_max: number;
  readonly ttl_s: number;
  readonly text?: string;
  readonly file?: FileVector;
  readonly meta_json: string;
  readonly plaintext_len: number;
  readonly aad_hex: string;
  readonly object_len: number;
  readonly object_sha256: string;
  readonly object_b64?: string;
}

interface LinkVector {
  readonly name: string;
  readonly input: string;
  readonly valid: boolean;
  readonly canonical?: boolean;
  readonly id?: string;
  readonly kdf_id?: number;
  readonly s_hex?: string;
  readonly newer_version?: boolean;
  readonly retired_kdf?: boolean;
}

interface MetaVector {
  readonly name: string;
  readonly input: string;
  readonly valid: boolean;
  readonly canonical?: string;
  readonly text?: string;
  readonly file_name?: string;
  readonly file_mime?: string;
  readonly file_size?: number;
}

interface VectorFile {
  readonly spec: string;
  readonly generator: string;
  readonly scrypt_rfc7914: readonly ScryptVector[];
  readonly derive: readonly DeriveVector[];
  readonly envelope: readonly EnvelopeVector[];
  readonly link: readonly LinkVector[];
  readonly meta: readonly MetaVector[];
  readonly passphrase_strength: readonly StrengthVector[];
  readonly request: readonly RequestVector[];
  readonly pow: readonly PowVector[];
}

interface PowVector {
  readonly name: string;
  readonly challenge: string;
  readonly id: string;
  readonly counter: number;
  readonly digest_hex: string;
  readonly zero_bits: number;
  readonly token: string;
  readonly difficulty?: number;
}

interface StrengthVector {
  readonly name: string;
  readonly passphrase: string;
  readonly accepted: boolean;
}

export function loadVectors(): VectorFile {
  const text = readFileSync(new URL('../../../spec/vectors.json', import.meta.url), 'utf8');
  return JSON.parse(text) as VectorFile;
}

export function hexToBytes(hex: string): Uint8Array<ArrayBuffer> {
  if (hex.length % 2 !== 0 || !/^[\da-f]*$/.test(hex)) {
    throw new Error(`not lowercase hex: ${hex}`);
  }
  const out = new Uint8Array(hex.length / 2);
  for (let index = 0; index < out.length; index++) {
    out[index] = Number.parseInt(hex.slice(2 * index, 2 * index + 2), 16);
  }
  return out;
}

export function bytesToHex(bytes: Uint8Array): string {
  let out = '';
  for (const byte of bytes) {
    out += byte.toString(16).padStart(2, '0');
  }
  return out;
}

export async function sha256Hex(bytes: Uint8Array): Promise<string> {
  return bytesToHex(await hash(bytes));
}

function counterBytes(size: number): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(size);
  for (let index = 0; index < size; index++) {
    out[index] = index % 256;
  }
  return out;
}

export function fileBytes(file: FileVector): Uint8Array<ArrayBuffer> {
  const bytes = file.content === 'counter' ? counterBytes(file.size) : hexToBytes(file.content);
  if (bytes.length !== file.size) {
    throw new Error(`file content length ${bytes.length} differs from size ${file.size}`);
  }
  return bytes;
}

export function randomId(): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(ID_LEN));
}

export function withByteFlipped(bytes: Uint8Array, index: number, mask = 1): Uint8Array<ArrayBuffer> {
  const copy = concatenate(bytes);
  copy[index] = (copy[index] ?? 0) ^ mask;
  return copy;
}

export function thrownBy(run: () => unknown): unknown {
  try {
    run();
  } catch (error) {
    return error;
  }
  return undefined;
}
