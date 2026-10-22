import { describe, expect, it } from 'vitest';

import {
  KDF_NONE,
  KDF_PBKDF2,
  KDF_SCRYPT,
  KEY_LEN,
  KdfError,
  MalformedPassphraseError,
  TOKEN_LEN,
  deriveAll,
  deriveContent,
  deriveLink,
  derivePassphraseKey,
  hsalt,
  kdfRequiresPassphrase,
  newHeader,
  newId,
  newSecret,
  normalizePassphrase,
  open,
  seal,
  utf8,
  type KdfId,
} from '../src/index.ts';
import { bytesToHex } from './helpers.ts';

const composed = 'héslo s češtinou';
const decomposed = 'héslo s češtinou';

describe('content key K', () => {
  it('is a non-extractable AES-GCM CryptoKey for encrypt and decrypt only', async () => {
    const { content } = await deriveAll(newId(), newSecret(), KDF_NONE, '');
    expect(content.k).toBeInstanceOf(CryptoKey);
    expect(content.k.type).toBe('secret');
    expect(content.k.extractable).toBe(false);
    expect(content.k.algorithm).toEqual({ name: 'AES-GCM', length: KEY_LEN * 8 });
    expect([...content.k.usages].sort()).toEqual(['decrypt', 'encrypt']);
    await expect(crypto.subtle.exportKey('raw', content.k)).rejects.toThrow();
    await expect(crypto.subtle.exportKey('jwk', content.k)).rejects.toThrow();
  });
});

describe('tokens', () => {
  it('R, salt_pw, V and hsalt are 32-byte Uint8Arrays', async () => {
    const id = newId();
    const s = newSecret();
    const { link, content } = await deriveAll(id, s, KDF_NONE, '');
    for (const bytes of [link.r, link.saltPw, content.v, await hsalt(id)]) {
      expect(bytes).toBeInstanceOf(Uint8Array);
      expect(bytes).toHaveLength(TOKEN_LEN);
    }
    expect(link.r).not.toEqual(content.v);
  });

  it('differ per id and per secret', async () => {
    const id = newId();
    const s = newSecret();
    const a = await deriveLink(id, s);
    const b = await deriveLink(newId(), s);
    const c = await deriveLink(id, newSecret());
    expect(a.r).not.toEqual(b.r);
    expect(a.r).not.toEqual(c.r);
    expect(a.saltPw).not.toEqual(b.saltPw);
  });

  it('reject an id or secret of the wrong length', async () => {
    await expect(hsalt(new Uint8Array(15))).rejects.toThrow();
    await expect(deriveLink(newId(), new Uint8Array(31))).rejects.toThrow();
    await expect(deriveContent(new Uint8Array(17), newSecret(), new Uint8Array(0))).rejects.toThrow();
  });
});

describe('passphrase normalisation', () => {
  it('rejects a passphrase that is not well-formed Unicode', async () => {
    const salt = new Uint8Array(32);
    for (const kdfId of [KDF_SCRYPT, KDF_PBKDF2] as const) {
      await expect(derivePassphraseKey(kdfId, 'caf\ud800', salt)).rejects.toBeInstanceOf(MalformedPassphraseError);
      await expect(deriveAll(newId(), newSecret(), kdfId, 'caf\ud800')).rejects.toBeInstanceOf(MalformedPassphraseError);
    }
    await expect(derivePassphraseKey(KDF_NONE, 'caf\ud800', salt)).resolves.toHaveLength(0);
  });

  it('encodes the NFC form as UTF-8', () => {
    expect(composed).not.toBe(decomposed);
    expect(bytesToHex(normalizePassphrase(decomposed))).toBe(bytesToHex(utf8(composed)));
    expect(bytesToHex(normalizePassphrase(composed))).toBe(bytesToHex(utf8(composed)));
  });

  it('derives identical bytes for composed and decomposed input with PBKDF2', async () => {
    const id = newId();
    const s = newSecret();
    const a = await deriveAll(id, s, KDF_PBKDF2, composed);
    const b = await deriveAll(id, s, KDF_PBKDF2, decomposed);
    expect(a.content.v).toEqual(b.content.v);
    expect(bytesToHex(a.content.v)).not.toBe(bytesToHex((await deriveAll(id, s, KDF_PBKDF2, 'other')).content.v));
    const header = newHeader(KDF_PBKDF2);
    const policy = { opensMax: 1, ttlSeconds: 3600 };
    const object = await seal(a.content.k, header, id, policy, { meta: { text: 'nfc' } });
    const opened = await open(b.content.k, id, policy, KDF_PBKDF2, object);
    expect(opened.payload.meta.text).toBe('nfc');
  });

  it('derives identical bytes for composed and decomposed input with scrypt', async () => {
    const id = newId();
    const s = newSecret();
    const { saltPw } = await deriveLink(id, s);
    const a = await derivePassphraseKey(KDF_SCRYPT, composed, saltPw);
    const b = await derivePassphraseKey(KDF_SCRYPT, decomposed, saltPw);
    expect(a).toHaveLength(KEY_LEN);
    expect(a).toEqual(b);
  }, 60_000);
});

describe('kdf profiles', () => {
  it('profile 0 ignores the passphrase and needs none', async () => {
    const salt = new Uint8Array(32);
    expect(await derivePassphraseKey(KDF_NONE, 'anything', salt)).toHaveLength(0);
    expect(kdfRequiresPassphrase(KDF_NONE)).toBe(false);
    expect(kdfRequiresPassphrase(KDF_SCRYPT)).toBe(true);
    expect(kdfRequiresPassphrase(KDF_PBKDF2)).toBe(true);
    const id = newId();
    const s = newSecret();
    const a = await deriveAll(id, s, KDF_NONE, 'a');
    const b = await deriveAll(id, s, KDF_NONE, 'b');
    expect(a.content.v).toEqual(b.content.v);
  });

  it('rejects an unknown profile', async () => {
    const unknown = Number('3') as KdfId;
    await expect(derivePassphraseKey(unknown, 'x', new Uint8Array(32))).rejects.toBeInstanceOf(KdfError);
  });
});
