export { type Expected, objectHash, open, seal, sealWithCompression } from './envelope/envelope.ts';
export { type Header, newHeader, type Policy } from './envelope/header.ts';
export type { Meta } from './envelope/meta.ts';
export {
  EnvelopeRecoveryError,
  IntegrityError,
  KdfMismatchError,
  LinkReleasedError,
  LinkRetiredKdfError,
  LinkVersionError,
  LongPassphraseError,
  MalformedPassphraseError,
  OpaqueMessageError,
  PowTimeoutError,
  ServerAuthenticationError,
  TooLargeError,
} from './errors.ts';
export { type ContentKeys, deriveContent, deriveLink, derivePassphraseSalt, type LinkKeys } from './keys/derive.ts';
export {
  assertWellFormedPassphrase,
  KDF_NONE,
  KDF_SCRYPT,
  type KdfId,
  opaqueContext,
  opaquePassword,
} from './keys/kdf.ts';
export {
  type ClientLogin,
  createRegistrationRequest,
  finalizeRegistrationRequest,
  generateKE1,
  generateKE3,
  identityStretch,
  KE2_LENGTH,
  type OpaqueConfig,
  REGISTRATION_RESPONSE_LENGTH,
  type RegistrationState,
  type Stretch,
  wipeLogin,
} from './keys/opaque.ts';
export {
  ASSIGNED_UNICODE_VERSION,
  generatePassphrase,
  MIN_PASSPHRASE_CHARACTERS,
  passphraseProblem,
} from './keys/passphrase.ts';
export {
  FLAG_DEADLINE,
  HEADER_LENGTH,
  ID_LENGTH,
  MAX_FILE_BYTES,
  MAX_MIME_BYTES,
  MAX_OBJECT_BYTES,
  MAX_TEXT_BYTES,
  MIN_TTL_SECONDS,
  transferSeconds,
} from './limits.ts';
export { assertProfile, formatLink, isReadPath, type Link, parseFragment, parseLink } from './link/link.ts';
export { decodeBase64url, encodeBase64url, encodedLength } from './primitives/base64url.ts';
export { utf8 } from './primitives/bytes.ts';
export { idToString, newSecret, parseId } from './primitives/identifiers.ts';
export { MAX_POW_DIFFICULTY, solvePow } from './request/proof-of-work.ts';
export {
  formatShareKey,
  newShareKey,
  parseWrappedShare,
  type ShareKey,
  unwrapShare,
  wrapShare,
} from './request/share.ts';
export {
  authorizationHeader,
  CHALLENGE_LENGTH,
  newRequestKey,
  parseChallenge,
  PUBLIC_KEY_LENGTH,
  type Purpose,
  PURPOSE_MANAGE,
  PURPOSE_READ,
  type RequestComponents,
  type RequestKey,
  requestKey,
  requestMessage,
  signRequest,
  wipeRequestKey,
} from './request/signature.ts';
