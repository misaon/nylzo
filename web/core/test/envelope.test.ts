import { describe, expect, it, vi } from 'vitest';

import { newId } from '../src/ids.ts';
import {
  aad,
  AAD_LEN,
  FlagsError,
  type Header,
  HEADER_LEN,
  headerBytes,
  HeaderError,
  IntegrityError,
  IV_LEN,
  KDF_NONE,
  KDF_RETIRED,
  KDF_SCRYPT,
  KdfError,
  type KdfId,
  KdfMismatchError,
  MAX_FILE_BYTES,
  MAX_OBJECT_BYTES,
  MAX_TEXT_BYTES,
  type Meta,
  MetaError,
  newHeader,
  newSecret,
  open,
  PAD_FILE,
  paddedLength as paddedLengthgth,
  parseHeader,
  type Policy,
  PolicyError,
  seal,
  TAG_LEN,
  VERSION,
  VersionError,
} from '../src/index.ts';
import { deriveAll } from '../src/scrypt.ts';
import { bytesToHex, withByteFlipped } from './helpers.ts';

interface Sample {
  readonly id: Uint8Array;
  readonly s: Uint8Array;
  readonly k: CryptoKey;
  readonly header: Header;
  readonly policy: Policy;
  readonly object: Uint8Array;
}

const policy: Policy = { opensMax: 1, ttlSeconds: 86_400 };

function textMeta(text: string, file?: Uint8Array): Meta {
  return file === undefined
    ? { text }
    : { text, file: { name: 'f.bin', mime: 'application/octet-stream', size: file.length } };
}

async function sample(kdfId: KdfId, passphrase: string, file?: Uint8Array): Promise<Sample> {
  const id = newId();
  const s = newSecret();
  const { content } = await deriveAll(id, s, kdfId, passphrase);
  const header = newHeader(kdfId);
  const object = await seal(content.k, header, id, policy, { meta: textMeta('integrity', file), file });
  return { id, s, k: content.k, header, policy, object };
}

async function plaintextLength(meta: Meta, file?: Uint8Array): Promise<number> {
  const id = newId();
  const { content } = await deriveAll(id, newSecret(), KDF_NONE, '');
  const object = await seal(content.k, newHeader(KDF_NONE), id, policy, { meta, file });
  return object.length - HEADER_LEN - TAG_LEN;
}

describe('AK8: tampering is detected', () => {
  it('a flipped ciphertext bit fails integrity', async () => {
    const m = await sample(KDF_NONE, '');
    for (const index of [HEADER_LEN, HEADER_LEN + 100, m.object.length - 1]) {
      const tampered = withByteFlipped(m.object, index);
      await expect(open(m.k, m.id, m.policy, KDF_NONE, tampered)).rejects.toBeInstanceOf(IntegrityError);
    }
  });

  it('a flipped iv byte fails integrity', async () => {
    const m = await sample(KDF_NONE, '');
    const tampered = withByteFlipped(m.object, 5);
    await expect(open(m.k, m.id, m.policy, KDF_NONE, tampered)).rejects.toBeInstanceOf(IntegrityError);
  });

  it('a changed policy fails integrity', async () => {
    const m = await sample(KDF_NONE, '');
    const changed: Policy[] = [
      { opensMax: 2, ttlSeconds: m.policy.ttlSeconds },
      { opensMax: m.policy.opensMax, ttlSeconds: m.policy.ttlSeconds + 1 },
    ];
    for (const bad of changed) {
      await expect(open(m.k, m.id, bad, KDF_NONE, m.object)).rejects.toBeInstanceOf(IntegrityError);
    }
  });

  it('a different id fails integrity', async () => {
    const m = await sample(KDF_NONE, '');
    await expect(open(m.k, newId(), m.policy, KDF_NONE, m.object)).rejects.toBeInstanceOf(IntegrityError);
  });

  it('a different key fails integrity', async () => {
    const m = await sample(KDF_NONE, '');
    const other = await deriveAll(m.id, newSecret(), KDF_NONE, '');
    await expect(open(other.content.k, m.id, m.policy, KDF_NONE, m.object)).rejects.toBeInstanceOf(IntegrityError);
  });

  it('a wrong passphrase fails integrity', async () => {
    const m = await sample(KDF_SCRYPT, 'right');
    const wrong = await deriveAll(m.id, m.s, KDF_SCRYPT, 'wrong');
    await expect(open(wrong.content.k, m.id, m.policy, KDF_SCRYPT, m.object)).rejects.toBeInstanceOf(IntegrityError);
  }, 60_000);
});

describe('every message gets a fresh random iv', () => {
  it('repeats no iv and spreads every byte of it', () => {
    const samples = 4096;
    const seen = new Set<string>();
    const distinct = Array.from({ length: IV_LEN }, () => new Set<number>());
    for (let n = 0; n < samples; n++) {
      const { iv } = newHeader(KDF_NONE);
      const hex = bytesToHex(iv);
      expect(seen.has(hex), `iv ${hex} repeated`).toBe(false);
      seen.add(hex);
      for (const [index, byte] of iv.entries()) {
        distinct[index]?.add(byte);
      }
    }
    expect(Math.min(...distinct.map((values) => values.size))).toBeGreaterThanOrEqual(200);
  });
});

describe('open rejects malformed headers before decrypting', () => {
  it('version 2 is rejected', async () => {
    const m = await sample(KDF_NONE, '');
    const tampered = withByteFlipped(m.object, 0, 0x03);
    expect(tampered[0]).toBe(2);
    await expect(open(m.k, m.id, m.policy, KDF_NONE, tampered)).rejects.toBeInstanceOf(VersionError);
  });

  it('flags 1 is rejected', async () => {
    const m = await sample(KDF_NONE, '');
    const tampered = withByteFlipped(m.object, 2);
    await expect(open(m.k, m.id, m.policy, KDF_NONE, tampered)).rejects.toBeInstanceOf(FlagsError);
  });

  it('the retired profile 2 is rejected', async () => {
    const m = await sample(KDF_NONE, '');
    const tampered = withByteFlipped(m.object, 1, KDF_RETIRED);
    expect(tampered[1]).toBe(KDF_RETIRED);
    expect(() => parseHeader(tampered)).toThrow(KdfError);
    await expect(open(m.k, m.id, m.policy, KDF_RETIRED as KdfId, tampered)).rejects.toBeInstanceOf(KdfError);
    await expect(
      seal(m.k, { ...newHeader(KDF_NONE), kdfId: KDF_RETIRED as KdfId }, m.id, m.policy, { meta: textMeta('x') }),
    ).rejects.toBeInstanceOf(KdfError);
  });

  it('a kdf argument that differs from the header is rejected', async () => {
    const m = await sample(KDF_NONE, '');
    await expect(open(m.k, m.id, m.policy, KDF_SCRYPT, m.object)).rejects.toBeInstanceOf(KdfMismatchError);
  });

  it('a truncated object is rejected', async () => {
    const m = await sample(KDF_NONE, '');
    await expect(open(m.k, m.id, m.policy, KDF_NONE, m.object.subarray(0, HEADER_LEN + 3))).rejects.toBeInstanceOf(
      IntegrityError,
    );
    await expect(open(m.k, m.id, m.policy, KDF_NONE, m.object.subarray(0, HEADER_LEN - 1))).rejects.toBeInstanceOf(
      HeaderError,
    );
  });
});

describe('padding', () => {
  it('paddedLength rounds up to the unit', () => {
    const cases: [number, boolean, number][] = [
      [0, false, 0],
      [1, false, 4096],
      [4095, false, 4096],
      [4096, false, 4096],
      [4097, false, 8192],
      [1, true, 65_536],
      [65_536, true, 65_536],
      [65_537, true, 131_072],
    ];
    for (const [n, hasFile, want] of cases) {
      expect(paddedLengthgth(n, hasFile)).toBe(want);
    }
  });

  it('text plaintext of 4096 bytes stays in one block and 4097 bytes needs two', async () => {
    expect(await plaintextLength({ text: 'a'.repeat(4075) })).toBe(4096);
    expect(await plaintextLength({ text: 'a'.repeat(4076) })).toBe(8192);
    expect(await plaintextLength({ text: 't'.repeat(5000) })).toBe(8192);
  });

  it('file plaintext pads to multiples of 64 KiB', async () => {
    const expected: [number, number][] = [
      [0, 65_536],
      [1, 65_536],
      [4095, 65_536],
      [4096, 65_536],
      [4097, 65_536],
      [65_536, 131_072],
      [65_537, 131_072],
    ];
    for (const [size, want] of expected) {
      const file = new Uint8Array(size);
      const length = await plaintextLength(textMeta('integrity', file), file);
      expect(length % PAD_FILE).toBe(0);
      expect(length).toBe(want);
    }
  });
});

describe('seal validates its inputs', () => {
  it('rejects content over the limits', async () => {
    const id = newId();
    const { content } = await deriveAll(id, newSecret(), KDF_NONE, '');
    const header = newHeader(KDF_NONE);
    const big = new Uint8Array(MAX_FILE_BYTES + 1);
    const bigMeta: Meta = { file: { name: 'x', mime: '', size: big.length } };
    await expect(seal(content.k, header, id, policy, { meta: bigMeta, file: big })).rejects.toBeInstanceOf(MetaError);
    const text = 'x'.repeat(MAX_TEXT_BYTES + 1);
    await expect(seal(content.k, header, id, policy, { meta: { text } })).rejects.toBeInstanceOf(MetaError);
  });

  it('rejects a file whose length differs from meta.file.size', async () => {
    const id = newId();
    const { content } = await deriveAll(id, newSecret(), KDF_NONE, '');
    const header = newHeader(KDF_NONE);
    const meta: Meta = { file: { name: 'x', mime: '', size: 2 } };
    await expect(seal(content.k, header, id, policy, { meta, file: new Uint8Array(1) })).rejects.toBeInstanceOf(
      MetaError,
    );
    await expect(
      seal(content.k, header, id, policy, { meta: { text: 't' }, file: new Uint8Array(1) }),
    ).rejects.toBeInstanceOf(MetaError);
  });

  it('rejects an invalid policy, flags and iv', async () => {
    const id = newId();
    const { content } = await deriveAll(id, newSecret(), KDF_NONE, '');
    const header = newHeader(KDF_NONE);
    const payload = { meta: { text: 'policy' } };
    await expect(seal(content.k, header, id, { opensMax: 0, ttlSeconds: 60 }, payload)).rejects.toBeInstanceOf(
      PolicyError,
    );
    await expect(seal(content.k, header, id, { opensMax: 1, ttlSeconds: 59 }, payload)).rejects.toBeInstanceOf(
      PolicyError,
    );
    await expect(seal(content.k, header, id, { opensMax: 65_536, ttlSeconds: 60 }, payload)).rejects.toBeInstanceOf(
      PolicyError,
    );
    await expect(seal(content.k, header, id, { opensMax: 1.5, ttlSeconds: 60 }, payload)).rejects.toBeInstanceOf(
      PolicyError,
    );
    const flagged: Header = { ...header, flags: 1 };
    await expect(seal(content.k, flagged, id, policy, payload)).rejects.toBeInstanceOf(FlagsError);
    const versioned: Header = { ...header, version: 2 };
    await expect(seal(content.k, versioned, id, policy, payload)).rejects.toBeInstanceOf(VersionError);
    const shortIv: Header = { ...header, iv: new Uint8Array(11) };
    await expect(seal(content.k, shortIv, id, policy, payload)).rejects.toBeInstanceOf(HeaderError);
  });

  it('fits the maximal message into MAX_OBJECT_BYTES', async () => {
    const id = newId();
    const { content } = await deriveAll(id, newSecret(), KDF_NONE, '');
    const file = new Uint8Array(MAX_FILE_BYTES);
    const meta: Meta = {
      text: 'y'.repeat(MAX_TEXT_BYTES),
      file: { name: 'max.bin', mime: 'application/octet-stream', size: file.length },
    };
    const object = await seal(content.k, newHeader(KDF_NONE), id, policy, { meta, file });
    expect(object.length).toBeLessThanOrEqual(MAX_OBJECT_BYTES);
    const opened = await open(content.k, id, policy, KDF_NONE, object);
    expect(opened.payload.meta.text).toBe(meta.text);
    expect(opened.payload.file).toEqual(file);
  }, 60_000);
});

describe('header and aad', () => {
  it('round trips through bytes', () => {
    const header = newHeader(KDF_SCRYPT);
    const parsed = parseHeader(headerBytes(header));
    expect(parsed.version).toBe(VERSION);
    expect(parsed.kdfId).toBe(KDF_SCRYPT);
    expect(parsed.flags).toBe(0);
    expect(parsed.iv).toEqual(header.iv);
  });

  it('prefixes the aad with the header bytes', () => {
    const header = newHeader(KDF_NONE);
    const id = newId();
    const bytes = aad(header, id, { opensMax: 1, ttlSeconds: 60 });
    expect(bytes.length).toBe(AAD_LEN);
    expect(bytes.subarray(0, HEADER_LEN)).toEqual(headerBytes(header));
    expect(bytes.subarray(HEADER_LEN, HEADER_LEN + 16)).toEqual(id);
    expect([...bytes.subarray(HEADER_LEN + 16)]).toEqual([0, 1, 0, 0, 0, 60]);
  });

  it('starts every sealed object with the header', async () => {
    const m = await sample(KDF_NONE, '');
    expect(m.object.subarray(0, HEADER_LEN)).toEqual(headerBytes(m.header));
    const opened = await open(m.k, m.id, m.policy, KDF_NONE, m.object);
    expect(opened.payload.meta).toEqual({ text: 'integrity' });
    expect(opened.payload.file).toBeUndefined();
  });
});

describe('plaintext buffers', () => {
  it('are wiped after seal and open, and the opened file is its own copy', async () => {
    const file = new Uint8Array(5000).fill(7);
    const m = await sample(KDF_NONE, '', file);
    const plaintexts: Uint8Array[] = [];
    const encrypt = crypto.subtle.encrypt.bind(crypto.subtle);
    const decrypt = crypto.subtle.decrypt.bind(crypto.subtle);
    const spies = [
      vi.spyOn(crypto.subtle, 'encrypt').mockImplementation(async (algorithm, key, data) => {
        if (data instanceof Uint8Array) {
          plaintexts.push(data);
        }
        return encrypt(algorithm, key, data);
      }),
      vi.spyOn(crypto.subtle, 'decrypt').mockImplementation(async (algorithm, key, data) => {
        const plaintext = await decrypt(algorithm, key, data);
        plaintexts.push(new Uint8Array(plaintext));
        return plaintext;
      }),
    ];
    let opened;
    try {
      await seal(m.k, newHeader(KDF_NONE), m.id, policy, { meta: textMeta('wipe me', file), file });
      opened = await open(m.k, m.id, policy, KDF_NONE, m.object);
    } finally {
      for (const spy of spies) {
        spy.mockRestore();
      }
    }
    expect(plaintexts).toHaveLength(2);
    expect(
      plaintexts.every((plaintext) => plaintext.length > file.length && plaintext.every((byte) => byte === 0)),
    ).toBe(true);
    expect(opened.payload.file).toEqual(file);
    expect(opened.payload.meta.text).toBe('integrity');
  });
});
