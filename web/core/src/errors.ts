export class EncodingError extends Error {
  override readonly name = 'EncodingError';
  constructor(message = 'value is not canonical base64url of the expected length') {
    super(message);
  }
}

export class KdfError extends Error {
  override readonly name = 'KdfError';
  constructor(message = 'unknown kdf profile') {
    super(message);
  }
}

export type PassphraseProblem = 'common' | 'context' | 'repetitive' | 'sequential' | 'short' | 'unassigned';

export class WeakPassphraseError extends Error {
  override readonly name = 'WeakPassphraseError';
  readonly problem: PassphraseProblem;
  constructor(problem: PassphraseProblem) {
    super(`weak passphrase (${problem})`);
    this.problem = problem;
  }
}

export class MalformedPassphraseError extends Error {
  override readonly name = 'MalformedPassphraseError';
  constructor(message = 'passphrase is not well-formed Unicode') {
    super(message);
  }
}

export class HeaderError extends Error {
  override readonly name = 'HeaderError';
  constructor(message = 'invalid envelope header') {
    super(message);
  }
}

export class VersionError extends Error {
  override readonly name = 'VersionError';
  constructor(message = 'unsupported envelope version') {
    super(message);
  }
}

export class FlagsError extends Error {
  override readonly name = 'FlagsError';
  constructor(message = 'unsupported envelope flags') {
    super(message);
  }
}

export class KdfMismatchError extends Error {
  override readonly name = 'KdfMismatchError';
  constructor(message = 'kdf profile does not match the link fragment') {
    super(message);
  }
}

export class IntegrityError extends Error {
  override readonly name = 'IntegrityError';
  constructor(message = 'envelope integrity check failed') {
    super(message);
  }
}

export class PlaintextError extends Error {
  override readonly name = 'PlaintextError';
  constructor(message = 'malformed plaintext') {
    super(message);
  }
}

export class MetaError extends Error {
  override readonly name = 'MetaError';
  constructor(message = 'invalid meta') {
    super(message);
  }
}

export class LinkError extends Error {
  override readonly name: string = 'LinkError';
  constructor(message = 'invalid link') {
    super(message);
  }
}

export class LinkVersionError extends LinkError {
  override readonly name = 'LinkVersionError';
  constructor() {
    super('invalid link: a newer fragment version');
  }
}

export class LinkRetiredKdfError extends LinkError {
  override readonly name = 'LinkRetiredKdfError';
  constructor() {
    super('invalid link: a retired passphrase profile');
  }
}

export class LinkReleasedError extends LinkError {
  override readonly name = 'LinkReleasedError';
  constructor() {
    super('invalid link: a link from before protocol version 2, whose message the upgrade erased');
  }
}

export class PolicyError extends Error {
  override readonly name = 'PolicyError';
  constructor(message = 'invalid policy') {
    super(message);
  }
}

export class TooLargeError extends Error {
  override readonly name = 'TooLargeError';
  constructor(message = 'content exceeds limits') {
    super(message);
  }
}

export class ShareError extends Error {
  override readonly name = 'ShareError';
  constructor(message = 'the content key needs the expiry share') {
    super(message);
  }
}

export class PowError extends Error {
  override readonly name = 'PowError';
  constructor(message = 'proof-of-work difficulty out of range') {
    super(message);
  }
}

export class PowTimeoutError extends Error {
  override readonly name = 'PowTimeoutError';
  constructor(message = 'proof of work not found before the challenge window ends') {
    super(message);
  }
}
