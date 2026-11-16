import { encodeBase64url } from '@nylzo/core';

import { ResponseError } from '../../shared/api/api-errors.ts';
import { isObject } from '../../shared/api/responses.ts';
import type { Json } from '../../shared/api/responses.ts';
import { KeyTypeError } from './problems.ts';

export interface Registration {
  readonly credential_id: string;
  readonly client_data: string;
  readonly authenticator_data: string;
  readonly public_key: string;
  readonly algorithm: number;
}

export interface Assertion {
  readonly credential_id: string;
  readonly client_data: string;
  readonly authenticator_data: string;
  readonly signature: string;
}

function bytes(buffer: ArrayBuffer): string {
  return encodeBase64url(new Uint8Array(buffer));
}

function publicKeyOptions(answer: Json): Json {
  const options = answer['public_key'];
  if (!isObject(options)) {
    throw new ResponseError('the ceremony has no public_key options');
  }
  return options;
}

function credentialOf(credential: Credential | null): PublicKeyCredential {
  if (!(credential instanceof PublicKeyCredential)) {
    throw new DOMException('the browser returned no passkey', 'NotAllowedError');
  }
  return credential;
}

async function canSignWithEd25519(): Promise<boolean> {
  try {
    await crypto.subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify']);
    return true;
  } catch {
    return false;
  }
}

export async function passkeysSupported(): Promise<boolean> {
  return (
    typeof PublicKeyCredential === 'function' &&
    typeof PublicKeyCredential.parseCreationOptionsFromJSON === 'function' &&
    typeof PublicKeyCredential.parseRequestOptionsFromJSON === 'function' &&
    typeof navigator.credentials?.create === 'function' &&
    (await canSignWithEd25519())
  );
}

export async function createPasskey(answer: Json): Promise<Registration> {
  const publicKey = PublicKeyCredential.parseCreationOptionsFromJSON(
    // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- The browser parses and checks the options the server sent.
    publicKeyOptions(answer) as unknown as PublicKeyCredentialCreationOptionsJSON,
  );
  const credential = credentialOf(await navigator.credentials.create({ publicKey }));
  const { response } = credential;
  if (!(response instanceof AuthenticatorAttestationResponse)) {
    throw new ResponseError('the passkey answered without an attestation');
  }
  const key = response.getPublicKey();
  if (key === null) {
    throw new KeyTypeError();
  }
  return {
    credential_id: bytes(credential.rawId),
    client_data: bytes(response.clientDataJSON),
    authenticator_data: bytes(response.getAuthenticatorData()),
    public_key: bytes(key),
    algorithm: response.getPublicKeyAlgorithm(),
  };
}

export async function usePasskey(answer: Json): Promise<Assertion> {
  const publicKey = PublicKeyCredential.parseRequestOptionsFromJSON(
    // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- The browser parses and checks the options the server sent.
    publicKeyOptions(answer) as unknown as PublicKeyCredentialRequestOptionsJSON,
  );
  const credential = credentialOf(await navigator.credentials.get({ publicKey }));
  const { response } = credential;
  if (!(response instanceof AuthenticatorAssertionResponse)) {
    throw new ResponseError('the passkey answered without an assertion');
  }
  return {
    credential_id: bytes(credential.rawId),
    client_data: bytes(response.clientDataJSON),
    authenticator_data: bytes(response.authenticatorData),
    signature: bytes(response.signature),
  };
}
