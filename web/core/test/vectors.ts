import { readFileSync } from 'node:fs';

import { isKdfId, type KdfId } from '../src/keys/kdf.ts';
import { ID_LENGTH } from '../src/limits.ts';
import { hash } from '../src/primitives/identifiers.ts';

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

interface StrengthVector {
  readonly name: string;
  readonly passphrase: string;
  readonly accepted: boolean;
  readonly problem?: string;
}

interface RequestVector {
  readonly name: string;
  readonly seed_hex: string;
  readonly public_key: string;
  readonly purpose: 'manage' | 'read' | 'verifier';
  readonly method: string;
  readonly path: string;
  readonly challenge: string;
  readonly body_hex: string;
  readonly message_hex: string;
  readonly signature: string;
  readonly authorization?: string;
  readonly verifier_header?: string;
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

interface VectorFile {
  readonly spec: string;
  readonly scrypt_rfc7914: readonly ScryptVector[];
  readonly derive: readonly DeriveVector[];
  readonly envelope: readonly EnvelopeVector[];
  readonly link: readonly LinkVector[];
  readonly meta: readonly MetaVector[];
  readonly passphrase_strength: readonly StrengthVector[];
  readonly passphrase_list_sha256: string;
  readonly request: readonly RequestVector[];
  readonly pow: readonly PowVector[];
}

export const vectors = loadVectors();

function loadVectors(): VectorFile {
  const file = JSON.parse(readFileSync(new URL('../../../spec/vectors.json', import.meta.url), 'utf8')) as VectorFile;
  const sections = [file.derive, file.envelope, file.link, file.meta, file.passphrase_strength, file.request, file.pow];
  const complete =
    file.spec === 'nylzo/v1' &&
    file.scrypt_rfc7914.length === 4 &&
    sections.every((section) => section.length > 0) &&
    file.link.some((vector) => vector.canonical === true);
  if (!complete) {
    throw new Error('spec/vectors.json lacks the nylzo/v1 cases these tests check');
  }
  return file;
}

export function kdfIdOf(value: number): KdfId {
  if (!isKdfId(value)) {
    throw new Error(`vector uses unknown kdf_id ${value}`);
  }
  return value;
}

export function hexToBytes(hex: string): Uint8Array<ArrayBuffer> {
  if (hex.length % 2 !== 0 || !/^[\da-f]*$/.test(hex)) {
    throw new Error(`not lowercase hex: ${hex}`);
  }
  return Uint8Array.from({ length: hex.length / 2 }, (_, index) =>
    Number.parseInt(hex.slice(2 * index, 2 * index + 2), 16),
  );
}

export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

export async function sha256Hex(bytes: Uint8Array): Promise<string> {
  return bytesToHex(await hash(bytes));
}

export function fileBytes(file: FileVector): Uint8Array<ArrayBuffer> {
  const bytes =
    file.content === 'counter'
      ? Uint8Array.from({ length: file.size }, (_, index) => index % 256)
      : hexToBytes(file.content);
  if (bytes.length !== file.size) {
    throw new Error(`file content length ${bytes.length} differs from size ${file.size}`);
  }
  return bytes;
}

export function randomId(): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(ID_LENGTH));
}

export function thrownBy(run: () => unknown): unknown {
  try {
    run();
  } catch (error) {
    return error;
  }
  return undefined;
}
