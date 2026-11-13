import {
  EnvelopeRecoveryError,
  generateKE1,
  generateKE3,
  identityStretch,
  KDF_NONE,
  LongPassphraseError,
  MalformedPassphraseError,
  opaqueContext,
  ServerAuthenticationError,
  wipeLogin,
} from '@nylzo/core';
import type { KdfId, RequestKey, Stretch } from '@nylzo/core';

import { isApiError } from '../../shared/api/api-errors.ts';
import type { Api, Login, Unlock } from '../../shared/api/api.ts';
import type { Progress } from '../../shared/kdf/kdf.ts';

export interface Attempts {
  readonly attemptsLeft: number;
  readonly retryAfter: number;
}

export type PassphraseRejection =
  | { readonly reason: 'long' }
  | { readonly reason: 'malformed' }
  | { readonly reason: 'wrong'; readonly attemptsLeft: number; readonly retryAfter: number; readonly locked: boolean };
export type WrongPassphrase = Extract<PassphraseRejection, { readonly reason: 'wrong' }>;

// A passphrase the core refuses before any derivation, so no attempt is counted.
export function passphraseRejection(error: unknown): PassphraseRejection | undefined {
  if (error instanceof MalformedPassphraseError) {
    return { reason: 'malformed' };
  }
  return error instanceof LongPassphraseError ? { reason: 'long' } : undefined;
}

// The envelope did not open, so the handshake that counted this attempt stays counted (spec 2.1).
class WrongPassphraseError extends Error {
  override readonly name = 'WrongPassphraseError';
  readonly attempts: Attempts;

  constructor(attempts: Attempts) {
    super('wrong passphrase');
    this.attempts = attempts;
  }
}

export function attemptsOf(error: unknown): Attempts | undefined {
  if (error instanceof WrongPassphraseError) {
    return error.attempts;
  }
  return isApiError(error, 'passphrase')
    ? { attemptsLeft: error.attemptsLeft ?? 0, retryAfter: error.retryAfter ?? 0 }
    : undefined;
}

// Each claim or burn runs its own handshake, and P waits as bytes until the claim brings the share (spec 2, 6.4).
export interface ContentAccess {
  readonly password: Uint8Array<ArrayBuffer>;
  readonly exportKey: () => Uint8Array<ArrayBuffer> | undefined;
  readonly keepExportKey: (exportKey: Uint8Array<ArrayBuffer>) => void;
}

export function accessFor(password: Uint8Array<ArrayBuffer>): ContentAccess {
  let kept: Uint8Array<ArrayBuffer> | undefined;
  return {
    password,
    exportKey: () => kept,
    keepExportKey: (exportKey) => {
      kept?.fill(0);
      kept = exportKey;
    },
  };
}

export function discardExportKey(exportKey: Uint8Array): void {
  exportKey.fill(0);
}

export interface LoginTarget {
  readonly api: Api;
  readonly id: Uint8Array<ArrayBuffer>;
  readonly kdfId: KdfId;
  readonly readKey: RequestKey;
  readonly stretch: Stretch;
  readonly lease?: string | undefined;
}

// A handshake for every attempt: the claim or burn presents its challenge, so a retry needs a new one (spec 6.4).
export function unlockWith(
  target: LoginTarget,
  password: Uint8Array,
  onExportKey: (exportKey: Uint8Array<ArrayBuffer>) => void,
): Unlock {
  const config = { context: opaqueContext(target.id, target.kdfId), stretch: target.stretch };
  return async (): Promise<Login> => {
    const { ke1, login } = generateKE1(password);
    try {
      const handshake = await target.api.handshake(target.id, target.readKey, ke1, target.lease);
      const result = await generateKE3(login, handshake.ke2, config).catch((error: unknown) => {
        if (!(error instanceof EnvelopeRecoveryError)) {
          throw error;
        }
        // Without a passphrase only a server with other keys can leave the envelope shut.
        throw target.kdfId === KDF_NONE ? new ServerAuthenticationError() : new WrongPassphraseError(handshake);
      });
      result.sessionKey.fill(0);
      onExportKey(result.exportKey);
      return { challenge: handshake.challenge, ke3: result.ke3 };
    } finally {
      wipeLogin(login);
    }
  };
}

export interface RememberedStretch {
  readonly around: (stretch: Stretch) => Stretch;
  readonly forget: () => void;
}

// The handshake of a burn after the reveal gives the same OPRF output, so the stretching of the reveal serves it
// without a second scrypt (spec 6.6).
export function rememberStretch(): RememberedStretch {
  let kept: { readonly input: Uint8Array; readonly output: Uint8Array } | undefined;
  const forget = (): void => {
    kept?.input.fill(0);
    kept?.output.fill(0);
    kept = undefined;
  };
  return {
    around: (stretch) => async (message) => {
      if (kept && sameBytes(kept.input, message)) {
        return new Uint8Array(kept.output);
      }
      const output = await stretch(message);
      forget();
      kept = { input: new Uint8Array(message), output: new Uint8Array(output) };
      return output;
    },
    forget,
  };
}

function sameBytes(first: Uint8Array, second: Uint8Array): boolean {
  return first.length === second.length && first.every((byte, index) => byte === second[index]);
}

interface KdfMark {
  readonly begin: () => void;
  readonly end: () => void;
}

interface MarkStore {
  readonly setItem: (key: string, value: string) => void;
  readonly removeItem: (key: string) => void;
}

// Marks a scrypt run, so a tab that crashed during one explains it when that message opens again; it holds the lease
// tag, never the message id.
export const KDF_MARK = 'nylzo:kdf';

export function kdfMark(session: Readonly<MarkStore>, owner: () => string): KdfMark {
  return {
    begin: () => quietly(() => session.setItem(KDF_MARK, owner())),
    end: () => quietly(() => session.removeItem(KDF_MARK)),
  };
}

function quietly(action: () => void): void {
  try {
    action();
  } catch {}
}

// Profile 3 stretches the OPRF output in a worker, between the begin and the end of the mark.
export function stretchFor(
  kdfId: KdfId,
  mark: KdfMark,
  stretch: (message: Uint8Array<ArrayBuffer>, onProgress: Progress) => Promise<Uint8Array<ArrayBuffer>>,
  onProgress?: Progress,
): Stretch {
  if (kdfId === KDF_NONE) {
    return identityStretch;
  }
  return async (message) => {
    onProgress?.(0);
    mark.begin();
    try {
      return await stretch(message, (fraction) => onProgress?.(fraction));
    } finally {
      mark.end();
    }
  };
}
