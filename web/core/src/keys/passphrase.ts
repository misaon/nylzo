import { MalformedPassphraseError, type PassphraseProblem, WeakPassphraseError } from '../errors.ts';
import { COMMON_PASSPHRASES } from './common-passphrases.ts';

export const MIN_PASSPHRASE_CHARACTERS = 12;
const MIN_DISTINCT_CHARACTERS = 6;
const CONTEXT_WORD = 'nylzo';
const GENERATED_GROUPS = 4;
const GENERATED_GROUP_LENGTH = 4;
const GENERATED_ALPHABET = '0123456789abcdefghjkmnpqrstvwxyz';
const SEQUENCES = [
  'abcdefghijklmnopqrstuvwxyz',
  '01234567890123456789',
  'qwertyuiopasdfghjklzxcvbnm',
  'qwertzuiopasdfghjklyxcvbnm',
] as const;

const common = new Set(COMMON_PASSPHRASES);

// Applies spec section 2 in its order, so both cores name the same problem.
export function assertStrongPassphrase(passphrase: string): void {
  if (!passphrase.isWellFormed()) {
    throw new MalformedPassphraseError();
  }
  const problem = passphraseProblem(passphrase.normalize('NFC'));
  if (problem) {
    throw new WeakPassphraseError(problem);
  }
}

export function generatePassphrase(): string {
  for (;;) {
    const random = crypto.getRandomValues(new Uint8Array(GENERATED_GROUPS * GENERATED_GROUP_LENGTH));
    const characters = Array.from(random, (value) => GENERATED_ALPHABET.charAt(value & 31));
    random.fill(0);
    const groups = Array.from({ length: GENERATED_GROUPS }, (_, group) =>
      characters.slice(group * GENERATED_GROUP_LENGTH, (group + 1) * GENERATED_GROUP_LENGTH).join(''),
    );
    const candidate = groups.join('-');
    if (passphraseProblem(candidate) === undefined) {
      return candidate;
    }
  }
}

function passphraseProblem(normalized: string): PassphraseProblem | undefined {
  if (codePoints(normalized).length < MIN_PASSPHRASE_CHARACTERS) {
    return 'short';
  }
  const folded = normalized.replaceAll(/[A-Z]/gu, (letter) => letter.toLowerCase());
  const characters = codePoints(folded);
  if (new Set(characters).size < MIN_DISTINCT_CHARACTERS || repeatsShorterPart(characters)) {
    return 'repetitive';
  }
  if (followsSequence(folded.replaceAll(/[ ._-]/gu, ''))) {
    return 'sequential';
  }
  if (codePoints(folded.replaceAll(CONTEXT_WORD, '')).length < MIN_PASSPHRASE_CHARACTERS) {
    return 'context';
  }
  return common.has(folded) ? 'common' : undefined;
}

function codePoints(text: string): string[] {
  // eslint-disable-next-line @typescript-eslint/no-misused-spread -- spec 2 counts code points, not graphemes
  return [...text];
}

function repeatsShorterPart(characters: readonly string[]): boolean {
  for (let period = 1; period <= characters.length / 2; period++) {
    if (characters.every((character, index) => index < period || character === characters[index - period])) {
      return true;
    }
  }
  return false;
}

function followsSequence(text: string): boolean {
  const backwards = codePoints(text).toReversed().join('');
  return SEQUENCES.some((sequence) => sequence.includes(text) || sequence.includes(backwards));
}
