import { ristretto255 } from '@noble/curves/ed25519.js';
import { expand } from '@noble/hashes/hkdf.js';
import { hmac } from '@noble/hashes/hmac.js';
import { sha512 } from '@noble/hashes/sha2.js';

import {
  deriveKeyPair,
  deriveKeys,
  deserializeElement,
  ELEMENT_LENGTH,
  HASH_LENGTH,
  INFO_DIFFIE_HELLMAN,
  labelled,
  MASKED_RESPONSE_LENGTH,
  NONCE_LENGTH,
  preambleOf,
  SCALAR_LENGTH,
  SEED_LENGTH,
} from '../keys/opaque.ts';
import { concatenate, utf8 } from '../primitives/bytes.ts';

// The server steps of OPAQUE-3DH (RFC 9807), for the fake server of the client tests only; the Go server is the
// one that runs.

const { Point } = ristretto255;

export interface ServerKeys {
  readonly privateKey: bigint;
  readonly publicKey: Uint8Array;
  readonly oprfSeed: Uint8Array;
}

export interface ServerRandomness {
  readonly maskingNonce: Uint8Array;
  readonly serverNonce: Uint8Array;
  readonly keyshareSeed: Uint8Array;
}

export function serverKeysFrom(privateKey: Uint8Array, oprfSeed: Uint8Array): ServerKeys {
  const secret = Point.Fn.fromBytes(privateKey);
  return { privateKey: secret, publicKey: Point.BASE.multiply(secret).toBytes(), oprfSeed };
}

export function newServerKeys(): ServerKeys {
  const { secret, publicKey } = deriveKeyPair(randomBytes(SEED_LENGTH), INFO_DIFFIE_HELLMAN);
  return { privateKey: secret, publicKey, oprfSeed: randomBytes(HASH_LENGTH) };
}

function randomBytes(length: number): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(length));
}

function oprfKey(keys: ServerKeys, credentialIdentifier: Uint8Array): bigint {
  const seed = expand(sha512, keys.oprfSeed, concatenate(credentialIdentifier, utf8('OprfKey')), SCALAR_LENGTH);
  return deriveKeyPair(seed, utf8('OPAQUE-DeriveKeyPair')).secret;
}

function evaluate(keys: ServerKeys, credentialIdentifier: Uint8Array, blinded: Uint8Array): Uint8Array {
  return deserializeElement(blinded).multiply(oprfKey(keys, credentialIdentifier)).toBytes();
}

export function createRegistrationResponse(
  request: Uint8Array,
  keys: ServerKeys,
  credentialIdentifier: Uint8Array,
): Uint8Array<ArrayBuffer> {
  return concatenate(evaluate(keys, credentialIdentifier, request), keys.publicKey);
}

export function generateKE2(
  keys: ServerKeys,
  record: Uint8Array,
  credentialIdentifier: Uint8Array,
  ke1: Uint8Array,
  context: Uint8Array,
  fixed?: ServerRandomness,
): { readonly ke2: Uint8Array<ArrayBuffer>; readonly expectedClientMac: Uint8Array } {
  const random = fixed ?? {
    maskingNonce: randomBytes(NONCE_LENGTH),
    serverNonce: randomBytes(NONCE_LENGTH),
    keyshareSeed: randomBytes(SEED_LENGTH),
  };
  const clientPublicKey = record.subarray(0, ELEMENT_LENGTH);
  const maskingKey = record.subarray(ELEMENT_LENGTH, ELEMENT_LENGTH + HASH_LENGTH);
  const envelope = record.subarray(ELEMENT_LENGTH + HASH_LENGTH);
  const clientKeyshare = deserializeElement(ke1.subarray(ELEMENT_LENGTH + NONCE_LENGTH));
  const pad = expand(
    sha512,
    maskingKey,
    labelled(random.maskingNonce, 'CredentialResponsePad'),
    MASKED_RESPONSE_LENGTH,
  );
  const plain = concatenate(keys.publicKey, envelope);
  const credentialResponse = concatenate(
    evaluate(keys, credentialIdentifier, ke1.subarray(0, ELEMENT_LENGTH)),
    random.maskingNonce,
    pad.map((byte, index) => byte ^ (plain[index] ?? 0)),
  );
  const { secret, publicKey: keyshare } = deriveKeyPair(random.keyshareSeed, INFO_DIFFIE_HELLMAN);
  const preamble = preambleOf(
    context,
    clientPublicKey,
    ke1,
    keys.publicKey,
    credentialResponse,
    random.serverNonce,
    keyshare,
  );
  const ikm = concatenate(
    clientKeyshare.multiply(secret).toBytes(),
    clientKeyshare.multiply(keys.privateKey).toBytes(),
    deserializeElement(clientPublicKey).multiply(secret).toBytes(),
  );
  const derived = deriveKeys(ikm, preamble);
  const serverMac = hmac(sha512, derived.serverMac, sha512(preamble));
  return {
    ke2: concatenate(credentialResponse, random.serverNonce, keyshare, serverMac),
    expectedClientMac: hmac(sha512, derived.clientMac, sha512(concatenate(preamble, serverMac))),
  };
}
