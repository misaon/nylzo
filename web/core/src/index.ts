export { open, type Opened, type OpenedPayload, type Payload, seal } from './envelope/envelope.ts';
export { type Header, newHeader, type Policy } from './envelope/header.ts';
export type { FileMeta, Meta } from './envelope/meta.ts';
export {
  EncodingError,
  FlagsError,
  HeaderError,
  IntegrityError,
  KdfError,
  KdfMismatchError,
  LinkError,
  LinkRetiredKdfError,
  LinkVersionError,
  MalformedPassphraseError,
  MetaError,
  PlaintextError,
  PolicyError,
  PowError,
  PowTimeoutError,
  TooLargeError,
  type PassphraseProblem,
  VersionError,
  WeakPassphraseError,
} from './errors.ts';
export { type ContentKeys, deriveContent, deriveLink, type LinkKeys } from './keys/derive.ts';
export { assertWellFormedPassphrase, KDF_NONE, KDF_SCRYPT, type KdfId } from './keys/kdf.ts';
export { assertStrongPassphrase, generatePassphrase, MIN_PASSPHRASE_CHARACTERS } from './keys/passphrase.ts';
export {
  HEADER_LENGTH,
  ID_LENGTH,
  MAX_FILE_BYTES,
  MAX_MIME_BYTES,
  MAX_OBJECT_BYTES,
  MAX_TEXT_BYTES,
  MIN_TTL_SECONDS,
  transferSeconds,
} from './limits.ts';
export {
  assertProfile,
  formatLink,
  type Fragment,
  type Link,
  parseFragment,
  parseLink,
  parsePath,
} from './link/link.ts';
export { encodeBase64url, encodedLength } from './primitives/base64url.ts';
export { utf8 } from './primitives/bytes.ts';
export { idToString, newSecret, parseId } from './primitives/identifiers.ts';
export { type SolveOptions, solvePow } from './request/proof-of-work.ts';
export {
  authorizationHeader,
  CHALLENGE_LENGTH,
  type CryptoRequestKey,
  newRequestKey,
  parseChallenge,
  PUBLIC_KEY_LENGTH,
  type Purpose,
  PURPOSE_MANAGE,
  PURPOSE_READ,
  PURPOSE_VERIFIER,
  type RequestKey,
  requestKey,
  requestMessage,
  type RequestSignature,
  type SeedRequestKey,
  signRequest,
  verifierHeader,
  wipeRequestKey,
} from './request/signature.ts';
