import { ristretto255, ristretto255_hasher } from '@noble/curves/ed25519.js';
import { bytesToNumberLE, equalBytes } from '@noble/curves/utils.js';
import { expand, extract } from '@noble/hashes/hkdf.js';
import { hmac } from '@noble/hashes/hmac.js';
import { sha512 } from '@noble/hashes/sha2.js';

import { EnvelopeRecoveryError, OpaqueMessageError, ServerAuthenticationError } from '../errors.ts';
import { MAX_OPRF_INPUT_LENGTH } from '../limits.ts';
import { arrayBufferBacked, concatenate, utf8 } from '../primitives/bytes.ts';

// OPAQUE-3DH of RFC 9807 with the OPRF ristretto255-SHA512 of RFC 9497, client steps only (spec section 2.1).

const { Point } = ristretto255;
const { Fn } = Point;
type Element = InstanceType<typeof Point>;

export const ELEMENT_LENGTH = 32;
export const SCALAR_LENGTH = 32;
export const NONCE_LENGTH = 32;
export const SEED_LENGTH = 32;
export const HASH_LENGTH = 64;
const MAC_LENGTH = 64;
const ENVELOPE_LENGTH = NONCE_LENGTH + MAC_LENGTH;
export const MASKED_RESPONSE_LENGTH = ELEMENT_LENGTH + ENVELOPE_LENGTH;
const CREDENTIAL_RESPONSE_LENGTH = ELEMENT_LENGTH + NONCE_LENGTH + MASKED_RESPONSE_LENGTH;
export const REGISTRATION_RESPONSE_LENGTH = 2 * ELEMENT_LENGTH;
export const KE2_LENGTH = CREDENTIAL_RESPONSE_LENGTH + NONCE_LENGTH + ELEMENT_LENGTH + MAC_LENGTH;
const UNIFORM_LENGTH = 64;
const MAX_DERIVE_COUNTER = 255;

const CONTEXT_STRING = concatenate(utf8('OPRFV1-'), Uint8Array.of(0), utf8('-ristretto255-SHA512'));
const DST_HASH_TO_GROUP = concatenate(utf8('HashToGroup-'), CONTEXT_STRING);
const DST_DERIVE_KEY_PAIR = concatenate(utf8('DeriveKeyPair'), CONTEXT_STRING);
export const INFO_DIFFIE_HELLMAN = utf8('OPAQUE-DeriveDiffieHellmanKeyPair');

export type Stretch = (message: Uint8Array<ArrayBuffer>) => Promise<Uint8Array>;

export interface OpaqueConfig {
  readonly context: Uint8Array;
  readonly stretch: Stretch;
}

export interface RegistrationState {
  readonly password: Uint8Array<ArrayBuffer>;
  readonly blind: bigint;
}

export interface ClientLogin {
  readonly password: Uint8Array<ArrayBuffer>;
  readonly blind: bigint;
  readonly clientSecret: bigint;
  readonly ke1: Uint8Array<ArrayBuffer>;
}

export interface Registration {
  readonly record: Uint8Array<ArrayBuffer>;
  readonly exportKey: Uint8Array<ArrayBuffer>;
}

export interface LoginResult {
  readonly ke3: Uint8Array<ArrayBuffer>;
  readonly sessionKey: Uint8Array<ArrayBuffer>;
  readonly exportKey: Uint8Array<ArrayBuffer>;
}

function u16(value: number): Uint8Array {
  if (value > MAX_OPRF_INPUT_LENGTH) {
    throw new OpaqueMessageError('the input is longer than 65535 bytes');
  }
  return Uint8Array.of(value >> 8, value & 0xff);
}

function withLength(field: Uint8Array): Uint8Array<ArrayBuffer> {
  return concatenate(u16(field.length), field);
}

// DeserializeElement of RFC 9497 section 4.1: a canonical encoding of an element other than the identity.
export function deserializeElement(bytes: Uint8Array): Element {
  if (bytes.length !== ELEMENT_LENGTH) {
    throw new OpaqueMessageError();
  }
  let element: Element;
  try {
    element = Point.fromBytes(bytes);
  } catch {
    throw new OpaqueMessageError();
  }
  if (element.equals(Point.ZERO)) {
    throw new OpaqueMessageError();
  }
  return element;
}

export function deriveKeyPair(
  seed: Uint8Array,
  info: Uint8Array,
): { readonly secret: bigint; readonly publicKey: Uint8Array } {
  const input = concatenate(seed, withLength(info), Uint8Array.of(0));
  try {
    for (let counter = 0; counter <= MAX_DERIVE_COUNTER; counter++) {
      input[input.length - 1] = counter;
      const secret = ristretto255_hasher.hashToScalar(input, { DST: DST_DERIVE_KEY_PAIR });
      if (!Fn.is0(secret)) {
        return { secret, publicKey: Point.BASE.multiply(secret).toBytes() };
      }
    }
  } finally {
    input.fill(0);
  }
  throw new OpaqueMessageError('no key pair derives from this seed');
}

function newBlind(): Uint8Array<ArrayBuffer> {
  const uniform = crypto.getRandomValues(new Uint8Array(UNIFORM_LENGTH));
  try {
    for (;;) {
      const scalar = Fn.create(bytesToNumberLE(uniform));
      if (!Fn.is0(scalar)) {
        return arrayBufferBacked(Fn.toBytes(scalar));
      }
      crypto.getRandomValues(uniform);
    }
  } finally {
    uniform.fill(0);
  }
}

function blindFrom(bytes: Uint8Array): bigint {
  let scalar: bigint;
  try {
    scalar = Fn.fromBytes(bytes);
  } catch {
    throw new OpaqueMessageError();
  }
  if (bytes.length !== SCALAR_LENGTH || Fn.is0(scalar)) {
    throw new OpaqueMessageError();
  }
  return scalar;
}

function blinded(input: Uint8Array, blind: bigint): Uint8Array<ArrayBuffer> {
  u16(input.length);
  const element = ristretto255_hasher.hashToCurve(input, { DST: DST_HASH_TO_GROUP });
  if (element.equals(Point.ZERO)) {
    throw new OpaqueMessageError('the input hashes to the identity');
  }
  return arrayBufferBacked(element.multiply(blind).toBytes());
}

// Finalize of RFC 9497 section 3.3.1.
function finalize(input: Uint8Array, blind: bigint, evaluated: Uint8Array): Uint8Array {
  const unblinded = deserializeElement(evaluated).multiply(Fn.inv(blind)).toBytes();
  return sha512(concatenate(withLength(input), withLength(unblinded), utf8('Finalize')));
}

async function randomizedPassword(
  password: Uint8Array,
  blind: bigint,
  evaluated: Uint8Array,
  stretch: Stretch,
): Promise<Uint8Array> {
  const output = finalize(password, blind, evaluated);
  const input = new Uint8Array(output);
  let stretched: Uint8Array | undefined;
  let ikm: Uint8Array | undefined;
  try {
    stretched = await stretch(input);
    ikm = concatenate(output, stretched);
    return extract(sha512, ikm);
  } finally {
    output.fill(0);
    input.fill(0);
    stretched?.fill(0);
    ikm?.fill(0);
  }
}

interface Stored {
  readonly envelope: Uint8Array<ArrayBuffer>;
  readonly clientPublicKey: Uint8Array;
  readonly clientSecret: bigint;
  readonly maskingKey: Uint8Array;
  readonly exportKey: Uint8Array;
}

function wipeStored(stored: Stored): void {
  stored.maskingKey.fill(0);
  stored.exportKey.fill(0);
}

export function labelled(nonce: Uint8Array, label: string): Uint8Array<ArrayBuffer> {
  return concatenate(nonce, utf8(label));
}

// CleartextCredentials with both identities left out, so each is its public key (RFC 9807 section 4).
function cleartextCredentials(serverPublicKey: Uint8Array, clientPublicKey: Uint8Array): Uint8Array<ArrayBuffer> {
  return concatenate(serverPublicKey, withLength(serverPublicKey), withLength(clientPublicKey));
}

// Store of RFC 9807 section 4.1.2 and, with the stored nonce, Recover of section 4.1.3.
function store(randomized: Uint8Array, serverPublicKey: Uint8Array, nonce: Uint8Array): Stored {
  const maskingKey = expand(sha512, randomized, utf8('MaskingKey'), HASH_LENGTH);
  const authKey = expand(sha512, randomized, labelled(nonce, 'AuthKey'), HASH_LENGTH);
  const exportKey = expand(sha512, randomized, labelled(nonce, 'ExportKey'), HASH_LENGTH);
  const seed = expand(sha512, randomized, labelled(nonce, 'PrivateKey'), SEED_LENGTH);
  try {
    const { secret, publicKey } = deriveKeyPair(seed, INFO_DIFFIE_HELLMAN);
    const tag = hmac(sha512, authKey, concatenate(nonce, cleartextCredentials(serverPublicKey, publicKey)));
    return {
      envelope: concatenate(nonce, tag),
      clientPublicKey: publicKey,
      clientSecret: secret,
      maskingKey,
      exportKey,
    };
  } finally {
    authKey.fill(0);
    seed.fill(0);
  }
}

function randomBytes(length: number): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(length));
}

// Bytes this module drew itself are wiped once read; test vectors pass their own.
function readOnce<Value>(
  given: Uint8Array | undefined,
  draw: () => Uint8Array,
  read: (bytes: Uint8Array) => Value,
): Value {
  const bytes = given ?? draw();
  try {
    return read(bytes);
  } finally {
    if (given === undefined) {
      bytes.fill(0);
    }
  }
}

// CreateRegistrationRequest of RFC 9807 section 5.2.1; `blind` is an input only for test vectors.
export function createRegistrationRequest(
  password: Uint8Array,
  blind?: Uint8Array,
): { readonly request: Uint8Array<ArrayBuffer>; readonly state: RegistrationState } {
  const scalar = readOnce(blind, newBlind, blindFrom);
  const request = blinded(password, scalar);
  return { request, state: { password: concatenate(password), blind: scalar } };
}

// FinalizeRegistrationRequest of RFC 9807 section 5.2.3; `envelopeNonce` is an input only for test vectors.
export async function finalizeRegistrationRequest(
  state: RegistrationState,
  response: Uint8Array,
  stretch: Stretch,
  envelopeNonce: Uint8Array = randomBytes(NONCE_LENGTH),
): Promise<Registration> {
  if (response.length !== REGISTRATION_RESPONSE_LENGTH || envelopeNonce.length !== NONCE_LENGTH) {
    throw new OpaqueMessageError();
  }
  const serverPublicKey = response.subarray(ELEMENT_LENGTH);
  deserializeElement(serverPublicKey);
  const randomized = await randomizedPassword(
    state.password,
    state.blind,
    response.subarray(0, ELEMENT_LENGTH),
    stretch,
  );
  try {
    const stored = store(randomized, serverPublicKey, envelopeNonce);
    try {
      return {
        record: concatenate(stored.clientPublicKey, stored.maskingKey, stored.envelope),
        exportKey: concatenate(stored.exportKey),
      };
    } finally {
      wipeStored(stored);
    }
  } finally {
    randomized.fill(0);
  }
}

// GenerateKE1 of RFC 9807 section 6.2.1; the randomness is an input only for test vectors.
export function generateKE1(
  password: Uint8Array,
  blind?: Uint8Array,
  clientNonce: Uint8Array = randomBytes(NONCE_LENGTH),
  keyshareSeed?: Uint8Array,
): { readonly ke1: Uint8Array<ArrayBuffer>; readonly login: ClientLogin } {
  if (clientNonce.length !== NONCE_LENGTH || (keyshareSeed !== undefined && keyshareSeed.length !== SEED_LENGTH)) {
    throw new OpaqueMessageError();
  }
  const scalar = readOnce(blind, newBlind, blindFrom);
  const request = blinded(password, scalar);
  const { secret, publicKey } = readOnce(
    keyshareSeed,
    () => randomBytes(SEED_LENGTH),
    (seed) => deriveKeyPair(seed, INFO_DIFFIE_HELLMAN),
  );
  const ke1 = concatenate(request, clientNonce, publicKey);
  return { ke1, login: { password: concatenate(password), blind: scalar, clientSecret: secret, ke1 } };
}

export function wipeLogin(login: ClientLogin | RegistrationState | undefined): void {
  login?.password.fill(0);
}

export function preambleOf(
  context: Uint8Array,
  clientPublicKey: Uint8Array,
  ke1: Uint8Array,
  serverPublicKey: Uint8Array,
  credentialResponse: Uint8Array,
  serverNonce: Uint8Array,
  serverKeyshare: Uint8Array,
): Uint8Array<ArrayBuffer> {
  return concatenate(
    utf8('OPAQUEv1-'),
    withLength(context),
    withLength(clientPublicKey),
    ke1,
    withLength(serverPublicKey),
    credentialResponse,
    serverNonce,
    serverKeyshare,
  );
}

// Expand-Label of RFC 9807 section 6.4.2.1.
function expandLabel(secret: Uint8Array, label: string, context: Uint8Array, length: number): Uint8Array {
  const full = utf8(`OPAQUE-${label}`);
  const info = concatenate(u16(length), Uint8Array.of(full.length), full, Uint8Array.of(context.length), context);
  return expand(sha512, secret, info, length);
}

export interface AkeKeys {
  readonly serverMac: Uint8Array;
  readonly clientMac: Uint8Array;
  readonly sessionKey: Uint8Array;
}

// DeriveKeys of RFC 9807 section 6.4.2.2.
export function deriveKeys(ikm: Uint8Array, preamble: Uint8Array): AkeKeys {
  const prk = extract(sha512, ikm);
  const transcript = sha512(preamble);
  const handshake = expandLabel(prk, 'HandshakeSecret', transcript, HASH_LENGTH);
  try {
    return {
      sessionKey: expandLabel(prk, 'SessionKey', transcript, HASH_LENGTH),
      serverMac: expandLabel(handshake, 'ServerMAC', new Uint8Array(0), HASH_LENGTH),
      clientMac: expandLabel(handshake, 'ClientMAC', new Uint8Array(0), HASH_LENGTH),
    };
  } finally {
    prk.fill(0);
    handshake.fill(0);
  }
}

async function recoverCredentials(
  login: ClientLogin,
  response: Uint8Array,
  stretch: Stretch,
): Promise<{ readonly stored: Stored; readonly serverPublicKey: Uint8Array }> {
  const randomized = await randomizedPassword(
    login.password,
    login.blind,
    response.subarray(0, ELEMENT_LENGTH),
    stretch,
  );
  const maskingKey = expand(sha512, randomized, utf8('MaskingKey'), HASH_LENGTH);
  const maskingNonce = response.subarray(ELEMENT_LENGTH, ELEMENT_LENGTH + NONCE_LENGTH);
  const pad = expand(sha512, maskingKey, labelled(maskingNonce, 'CredentialResponsePad'), MASKED_RESPONSE_LENGTH);
  try {
    const masked = response.subarray(ELEMENT_LENGTH + NONCE_LENGTH);
    const unmasked = pad.map((byte, index) => byte ^ (masked[index] ?? 0));
    const serverPublicKey = unmasked.subarray(0, ELEMENT_LENGTH);
    const envelope = unmasked.subarray(ELEMENT_LENGTH);
    const stored = store(randomized, serverPublicKey, envelope.subarray(0, NONCE_LENGTH));
    if (!equalBytes(stored.envelope, envelope)) {
      wipeStored(stored);
      throw new EnvelopeRecoveryError();
    }
    return { stored, serverPublicKey };
  } finally {
    randomized.fill(0);
    maskingKey.fill(0);
    pad.fill(0);
  }
}

// GenerateKE3 of RFC 9807 section 6.2.3: RecoverCredentials, then AuthClientFinalize.
export async function generateKE3(login: ClientLogin, ke2: Uint8Array, config: OpaqueConfig): Promise<LoginResult> {
  if (ke2.length !== KE2_LENGTH) {
    throw new OpaqueMessageError();
  }
  const response = ke2.subarray(0, CREDENTIAL_RESPONSE_LENGTH);
  const serverNonce = ke2.subarray(CREDENTIAL_RESPONSE_LENGTH, CREDENTIAL_RESPONSE_LENGTH + NONCE_LENGTH);
  const keyshareEnd = CREDENTIAL_RESPONSE_LENGTH + NONCE_LENGTH + ELEMENT_LENGTH;
  const serverKeyshare = ke2.subarray(CREDENTIAL_RESPONSE_LENGTH + NONCE_LENGTH, keyshareEnd);
  const serverMac = ke2.subarray(keyshareEnd);
  const serverKeyshareElement = deserializeElement(serverKeyshare);
  const { stored, serverPublicKey } = await recoverCredentials(login, response, config.stretch);
  try {
    const serverPublicElement = deserializeElement(serverPublicKey);
    const ikm = concatenate(
      serverKeyshareElement.multiply(login.clientSecret).toBytes(),
      serverPublicElement.multiply(login.clientSecret).toBytes(),
      serverKeyshareElement.multiply(stored.clientSecret).toBytes(),
    );
    const preamble = preambleOf(
      config.context,
      stored.clientPublicKey,
      login.ke1,
      serverPublicKey,
      response,
      serverNonce,
      serverKeyshare,
    );
    const keys = deriveKeys(ikm, preamble);
    ikm.fill(0);
    try {
      const expectedServerMac = hmac(sha512, keys.serverMac, sha512(preamble));
      if (!equalBytes(serverMac, expectedServerMac)) {
        throw new ServerAuthenticationError();
      }
      const transcript = sha512(concatenate(preamble, expectedServerMac));
      return {
        ke3: arrayBufferBacked(hmac(sha512, keys.clientMac, transcript)),
        sessionKey: concatenate(keys.sessionKey),
        exportKey: concatenate(stored.exportKey),
      };
    } finally {
      keys.serverMac.fill(0);
      keys.clientMac.fill(0);
      keys.sessionKey.fill(0);
    }
  } finally {
    wipeStored(stored);
  }
}

// The stretching function of the profile without a passphrase, and of the RFC 9807 test vectors.
export function identityStretch(message: Uint8Array): Promise<Uint8Array> {
  return Promise.resolve(new Uint8Array(message));
}
