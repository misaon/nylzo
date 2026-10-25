import { describe, expect, it, vi } from 'vitest';

import { bytesToHex, randomId } from '../../test/vectors.ts';
import { open, seal } from '../envelope/envelope.ts';
import { newHeader } from '../envelope/header.ts';
import { KdfError, MalformedPassphraseError } from '../errors.ts';
import { KEY_LEN, TOKEN_LEN } from '../limits.ts';
import { utf8 } from '../primitives/bytes.ts';
import { newSecret } from '../primitives/identifiers.ts';
import { anyPassphraseKey, deriveAll, scryptPassphraseKey } from '../scrypt.ts';
import { deriveContent, deriveLink, hsalt } from './derive.ts';
import { isKdfId, KDF_NONE, KDF_RETIRED, KDF_SCRYPT, type KdfId, normalizePassphrase, parseKdfId } from './kdf.ts';

const composed = 'héslo s češtinou';
const decomposed = 'héslo s češtinou';

describe('content key K', () => {
  it('is a non-extractable AES-GCM CryptoKey for encrypt and decrypt only', async () => {
    const { content } = await deriveAll(randomId(), newSecret(), KDF_NONE, '');
    expect(content.k).toBeInstanceOf(CryptoKey);
    expect(content.k.type).toBe('secret');
    expect(content.k.extractable).toBe(false);
    expect(content.k.algorithm).toEqual({ name: 'AES-GCM', length: KEY_LEN * 8 });
    expect(content.k.usages.toSorted((left, right) => left.localeCompare(right))).toEqual(['decrypt', 'encrypt']);
    await expect(crypto.subtle.exportKey('raw', content.k)).rejects.toThrow();
    await expect(crypto.subtle.exportKey('jwk', content.k)).rejects.toThrow();
  });
});

describe('tokens', () => {
  it('R, salt_pw, V and hsalt are 32-byte Uint8Arrays', async () => {
    const id = randomId();
    const s = newSecret();
    const { link, content } = await deriveAll(id, s, KDF_NONE, '');
    for (const bytes of [link.r, link.saltPw, content.v, await hsalt(id)]) {
      expect(bytes).toBeInstanceOf(Uint8Array);
      expect(bytes).toHaveLength(TOKEN_LEN);
    }
    expect(link.r).not.toEqual(content.v);
  });

  it('differ per id and per secret', async () => {
    const id = randomId();
    const s = newSecret();
    const a = await deriveLink(id, s);
    const b = await deriveLink(randomId(), s);
    const c = await deriveLink(id, newSecret());
    expect(a.r).not.toEqual(b.r);
    expect(a.r).not.toEqual(c.r);
    expect(a.saltPw).not.toEqual(b.saltPw);
  });

  it('reject an id or secret of the wrong length', async () => {
    await expect(hsalt(new Uint8Array(15))).rejects.toThrow();
    await expect(deriveLink(randomId(), new Uint8Array(31))).rejects.toThrow();
    await expect(deriveContent(new Uint8Array(17), newSecret(), new Uint8Array(0))).rejects.toThrow();
  });
});

describe('passphrase normalisation', () => {
  it('rejects a passphrase that is not well-formed Unicode', async () => {
    const salt = new Uint8Array(32);
    expect(() => scryptPassphraseKey('caf\u{D800}', salt)).toThrow(MalformedPassphraseError);
    await expect(anyPassphraseKey(KDF_SCRYPT, 'caf\u{D800}', salt)).rejects.toBeInstanceOf(MalformedPassphraseError);
    await expect(deriveAll(randomId(), newSecret(), KDF_SCRYPT, 'caf\u{D800}')).rejects.toBeInstanceOf(
      MalformedPassphraseError,
    );
    await expect(anyPassphraseKey(KDF_NONE, 'caf\u{D800}', salt)).resolves.toHaveLength(0);
  });

  it('encodes the NFC form as UTF-8', () => {
    expect(composed).not.toBe(decomposed);
    expect(bytesToHex(normalizePassphrase(decomposed))).toBe(bytesToHex(utf8(composed)));
    expect(bytesToHex(normalizePassphrase(composed))).toBe(bytesToHex(utf8(composed)));
  });

  it('derives identical keys for composed and decomposed input with scrypt', async () => {
    const id = randomId();
    const s = newSecret();
    const a = await deriveAll(id, s, KDF_SCRYPT, composed);
    const b = await deriveAll(id, s, KDF_SCRYPT, decomposed);
    expect(a.content.v).toEqual(b.content.v);
    const other = await deriveAll(id, s, KDF_SCRYPT, 'other');
    expect(bytesToHex(a.content.v)).not.toBe(bytesToHex(other.content.v));
    expect(scryptPassphraseKey(composed, a.link.saltPw)).toHaveLength(KEY_LEN);
    const header = newHeader(KDF_SCRYPT);
    const policy = { opensMax: 1, ttlSeconds: 3600 };
    const object = await seal(a.content.k, header, id, policy, { meta: { text: 'nfc' } });
    const opened = await open(b.content.k, id, policy, KDF_SCRYPT, object);
    expect(opened.payload.meta.text).toBe('nfc');
  }, 60_000);
});

describe('kdf profiles', () => {
  it('profile 0 ignores the passphrase and needs none', async () => {
    const salt = new Uint8Array(32);
    expect(await anyPassphraseKey(KDF_NONE, 'anything', salt)).toHaveLength(0);
    const id = randomId();
    const s = newSecret();
    const a = await deriveAll(id, s, KDF_NONE, 'a');
    const b = await deriveAll(id, s, KDF_NONE, 'b');
    expect(a.content.v).toEqual(b.content.v);
  });

  it('rejects the retired profile 2 and an unknown profile', async () => {
    expect(isKdfId(KDF_RETIRED)).toBe(false);
    expect(() => parseKdfId(String(KDF_RETIRED))).toThrow(KdfError);
    for (const kdfId of [KDF_RETIRED, Number('3')] as KdfId[]) {
      await expect(anyPassphraseKey(kdfId, 'correct horse battery staple', new Uint8Array(32))).rejects.toBeInstanceOf(
        KdfError,
      );
      await expect(deriveAll(randomId(), newSecret(), kdfId, 'correct horse battery staple')).rejects.toBeInstanceOf(
        KdfError,
      );
    }
  });

  it('wipes the copy of S and P it feeds into HKDF', async () => {
    const imported: Uint8Array[] = [];
    const importKey = crypto.subtle.importKey.bind(crypto.subtle);
    const spy = vi.spyOn(crypto.subtle, 'importKey').mockImplementation(((
      format: KeyFormat,
      data: BufferSource,
      ...rest: [AlgorithmIdentifier, boolean, KeyUsage[]]
    ) => {
      if (data instanceof Uint8Array && data.length === 64) {
        imported.push(data);
      }
      return importKey(format as 'raw', data, ...rest);
    }) as typeof crypto.subtle.importKey);
    try {
      await deriveContent(randomId(), newSecret(), new Uint8Array(32).fill(9));
    } finally {
      spy.mockRestore();
    }
    expect(imported).toEqual([new Uint8Array(64)]);
  });
});
