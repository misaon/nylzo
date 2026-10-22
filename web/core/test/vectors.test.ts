import { scryptAsync } from '@noble/hashes/scrypt.js';
import { describe, expect, it } from 'vitest';

import {
  HEADER_LEN,
  KDF_NONE,
  TAG_LEN,
  VERSION,
  aad,
  decodeMeta,
  deriveAll,
  deriveContent,
  deriveLink,
  derivePassphraseKey,
  encodeBase64url,
  encodeMeta,
  formatLink,
  fragment,
  hash,
  hsalt,
  idToString,
  isKdfId,
  open,
  parseId,
  parseLink,
  seal,
  utf8,
  type Header,
  type KdfId,
  type Meta,
  type Policy,
} from '../src/index.ts';
import { bytesToHex, fileBytes, hexToBytes, loadVectors, metaOf, sha256Hex } from './helpers.ts';

const vectors = loadVectors();
const slow = { timeout: 120_000 };
const heavyEnabled = process.env.NYLZO_HEAVY_TESTS === '1';

function kdfIdOf(value: number): KdfId {
  if (!isKdfId(value)) {
    throw new Error(`vector uses unknown kdf_id ${value}`);
  }
  return value;
}

describe('spec/vectors.json', () => {
  it('targets protocol v1', () => {
    expect(vectors.spec).toBe('nylzo/v1');
  });
});

describe('scrypt_rfc7914', () => {
  expect(vectors.scrypt_rfc7914).toHaveLength(4);
  for (const v of vectors.scrypt_rfc7914) {
    it.skipIf(v.heavy && !heavyEnabled)(v.name, { timeout: 600_000 }, async () => {
      const dk = await scryptAsync(utf8(v.password), utf8(v.salt), { N: v.N, r: v.r, p: v.p, dkLen: v.dkLen });
      expect(bytesToHex(dk)).toBe(v.dk_hex);
    });
  }
});

describe('derive', () => {
  for (const v of vectors.derive) {
    it(v.name, slow, async () => {
      const id = parseId(v.id);
      const s = hexToBytes(v.s);
      const kdfId = kdfIdOf(v.kdf_id);
      expect(bytesToHex(await hsalt(id))).toBe(v.hsalt_hex);

      const link = await deriveLink(id, s);
      expect(bytesToHex(link.r)).toBe(v.r_hex);
      expect(bytesToHex(link.saltPw)).toBe(v.salt_pw_hex);

      const p = await derivePassphraseKey(kdfId, v.passphrase ?? '', link.saltPw);
      expect(bytesToHex(p)).toBe(v.p_hex);

      const content = await deriveContent(id, s, p);
      expect(bytesToHex(content.v)).toBe(v.v_hex);
      expect(content.k.extractable).toBe(false);

      expect(encodeBase64url(link.r)).toBe(v.wire.r);
      expect(encodeBase64url(await hash(link.r))).toBe(v.wire.read_hash);
      expect(bytesToHex(await hash(link.r))).toBe(v.read_hash_hex);
      const m = hexToBytes(v.m_hex);
      expect(encodeBase64url(m)).toBe(v.wire.m);
      expect(encodeBase64url(await hash(m))).toBe(v.wire.manage_hash);
      expect(bytesToHex(await hash(m))).toBe(v.manage_hash_hex);
      if (kdfId === KDF_NONE) {
        expect(v.wire.verifier_hash).toBeUndefined();
      } else {
        expect(encodeBase64url(content.v)).toBe(v.wire.v);
        expect(encodeBase64url(await hash(content.v))).toBe(v.wire.verifier_hash);
        expect(bytesToHex(await hash(content.v))).toBe(v.verifier_hash_hex);
      }
      expect(fragment(kdfId, s)).toBe(v.fragment);

      const all = await deriveAll(id, s, kdfId, v.passphrase ?? '');
      expect(bytesToHex(all.link.r)).toBe(v.r_hex);
      expect(bytesToHex(all.content.v)).toBe(v.v_hex);
    });
  }

  it('NFC composed and decomposed passphrases agree', () => {
    const composed = vectors.derive.find((v) => v.name === 'scrypt-nfc-composed');
    const decomposed = vectors.derive.find((v) => v.name === 'scrypt-nfc-decomposed');
    if (composed === undefined || decomposed === undefined) {
      throw new Error('nfc vectors missing');
    }
    expect(composed.passphrase).not.toBe(decomposed.passphrase);
    expect(composed.p_hex).toBe(decomposed.p_hex);
    expect(composed.k_hex).toBe(decomposed.k_hex);
  });
});

describe('envelope', () => {
  for (const v of vectors.envelope) {
    it(v.name, slow, async () => {
      const id = parseId(v.id);
      const s = hexToBytes(v.s);
      const kdfId = kdfIdOf(v.kdf_id);
      const { content } = await deriveAll(id, s, kdfId, v.passphrase ?? '');
      const header: Header = { version: VERSION, kdfId, flags: v.flags, iv: hexToBytes(v.iv_hex) };
      const policy: Policy = { opensMax: v.opens_max, ttlSeconds: v.ttl_s };
      const file = v.file === undefined ? undefined : fileBytes(v.file);
      const meta: Meta = metaOf(
        v.text,
        v.file === undefined ? undefined : { name: v.file.name, mime: v.file.mime, size: v.file.size },
      );

      expect(new TextDecoder().decode(encodeMeta(meta))).toBe(v.meta_json);
      expect(bytesToHex(aad(header, id, policy))).toBe(v.aad_hex);

      const object = await seal(content.k, header, id, policy, { meta, file });
      expect(object.length).toBe(v.object_len);
      expect(object.length - HEADER_LEN - TAG_LEN).toBe(v.plaintext_len);
      expect(await sha256Hex(object)).toBe(v.object_sha256);
      if (v.object_b64 !== undefined) {
        expect(encodeBase64url(object)).toBe(v.object_b64);
      }

      const opened = await open(content.k, id, policy, kdfId, object);
      expect(opened.header.version).toBe(VERSION);
      expect(opened.header.kdfId).toBe(kdfId);
      expect(opened.header.flags).toBe(v.flags);
      expect(bytesToHex(opened.header.iv)).toBe(v.iv_hex);
      expect(opened.payload.meta.text).toBe(v.text);
      if (v.file === undefined) {
        expect(opened.payload.meta.file).toBeUndefined();
        expect(opened.payload.file).toBeUndefined();
      } else {
        expect(opened.payload.meta.file).toEqual({ name: v.file.name, mime: v.file.mime, size: v.file.size });
        expect(opened.payload.file).toBeInstanceOf(Uint8Array);
        expect(opened.payload.file).toEqual(file);
      }
    });
  }
});

describe('link', () => {
  it('has canonical vectors for the format round trip', () => {
    expect(vectors.link.filter((v) => v.canonical === true).length).toBeGreaterThan(0);
  });
  for (const v of vectors.link) {
    it(v.name, () => {
      if (!v.valid) {
        expect(() => parseLink(v.input)).toThrow();
        return;
      }
      const link = parseLink(v.input);
      expect(idToString(link.id)).toBe(v.id);
      expect(link.kdfId).toBe(v.kdf_id);
      expect(bytesToHex(link.s)).toBe(v.s_hex);
      if (v.canonical === true) {
        expect(formatLink('https://nylzo.example', link.id, link.kdfId, link.s)).toBe(v.input);
      }
    });
  }
});

describe('meta', () => {
  for (const v of vectors.meta) {
    it(v.name, () => {
      const input = utf8(v.input);
      if (!v.valid) {
        expect(() => decodeMeta(input)).toThrow();
        return;
      }
      const meta = decodeMeta(input);
      const canonical = encodeMeta(meta);
      expect(new TextDecoder().decode(canonical)).toBe(v.canonical);
      expect(meta.text).toBe(v.text);
      if (v.file_name === undefined) {
        expect(meta.file).toBeUndefined();
      } else {
        expect(meta.file).toEqual({ name: v.file_name, mime: v.file_mime, size: v.file_size });
      }
      expect(encodeMeta(decodeMeta(canonical))).toEqual(canonical);
    });
  }
});
