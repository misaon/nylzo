import { MalformedPassphraseError, WeakPassphraseError } from '../errors.ts';
import type { PassphraseProblem } from '../errors.ts';
import { COMMON_PASSPHRASES } from './common-passphrases.ts';
import { normalizePassphrase } from './kdf.ts';
import { loadPassphraseWords, WORD_LIST_LENGTH } from './passphrase-words.ts';
import type { PassphraseLanguage, PassphraseWords } from './passphrase-words.ts';
import { ASSIGNED_RANGE_DELTAS } from './unicode-assigned.ts';

export const MIN_PASSPHRASE_CHARACTERS = 15;
export const ASSIGNED_UNICODE_VERSION = '15.1';
const MIN_DISTINCT_CHARACTERS = 6;
const CONTEXT_WORD = 'nylzo';
const SEQUENCES = [
  'abcdefghijklmnopqrstuvwxyz',
  '01234567890123456789',
  'qwertyuiopasdfghjklzxcvbnm',
  'qwertzuiopasdfghjklyxcvbnm',
] as const;

const common = new Set(COMMON_PASSPHRASES);
function decodeRanges(deltas: string): readonly (readonly [number, number])[] {
  let nextStart = 0;
  return deltas.split(' ').map((delta) => {
    const separator = delta.indexOf(',');
    const first = nextStart + Number.parseInt(delta.slice(0, separator), 16);
    const last = first + Number.parseInt(delta.slice(separator + 1), 16);
    nextStart = last + 1;
    return [first, last] as const;
  });
}

const assignedRanges = decodeRanges(ASSIGNED_RANGE_DELTAS);

export function assignedCodePointRanges(): readonly (readonly [number, number])[] {
  return assignedRanges;
}

function codePointEscape(codePoint: number): string {
  return `\\u{${codePoint.toString(16)}}`;
}

// Unassigned in Unicode 15.1, whatever Unicode version this browser implements.
const UNASSIGNED = new RegExp(
  `[^${assignedRanges.map(([first, last]) => `${codePointEscape(first)}-${codePointEscape(last)}`).join('')}]`,
  'u',
);

// Applies spec section 2 in its order, so both cores name the same problem.
export function passphraseProblem(passphrase: string): PassphraseProblem | 'malformed' | undefined {
  if (!passphrase.isWellFormed()) {
    return 'malformed';
  }
  if (UNASSIGNED.test(passphrase)) {
    return 'unassigned';
  }
  return strengthProblem(normalizePassphrase(passphrase));
}

export function assertStrongPassphrase(passphrase: string): void {
  const problem = passphraseProblem(passphrase);
  if (problem === 'malformed') {
    throw new MalformedPassphraseError();
  }
  if (problem) {
    throw new WeakPassphraseError(problem);
  }
}

function wordAt(words: readonly string[], index: number): string {
  const word = words[index];
  if (word === undefined) {
    throw new Error(`no word at index ${index}`);
  }
  return word;
}

export function passphraseFrom(list: PassphraseWords, randomBytes: (length: number) => Uint8Array): string {
  for (;;) {
    const random = randomBytes(2 * list.count);
    const view = new DataView(random.buffer, random.byteOffset, random.byteLength);
    const chosen = Array.from({ length: list.count }, (_, index) =>
      wordAt(list.words, view.getUint16(2 * index) % WORD_LIST_LENGTH),
    );
    random.fill(0);
    const candidate = chosen.join(list.separator);
    if (passphraseProblem(candidate) === undefined) {
      return candidate;
    }
  }
}

export async function generatePassphrase(language: PassphraseLanguage): Promise<string> {
  const list = await loadPassphraseWords(language);
  return passphraseFrom(list, (length) => crypto.getRandomValues(new Uint8Array(length)));
}

function strengthProblem(normalized: string): PassphraseProblem | undefined {
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
  // oxlint-disable-next-line typescript/no-misused-spread -- spec 2 counts code points, not graphemes
  return [...text];
}

function repeatsShorterPart(characters: readonly string[]): boolean {
  const borders = new DataView(new ArrayBuffer(Uint32Array.BYTES_PER_ELEMENT * characters.length));
  const borderAt = (index: number): number => borders.getUint32(Uint32Array.BYTES_PER_ELEMENT * index);
  let border = 0;
  for (let index = 1; index < characters.length; index++) {
    while (border > 0 && characters[index] !== characters[border]) {
      border = borderAt(border - 1);
    }
    if (characters[index] === characters[border]) {
      border++;
    }
    borders.setUint32(Uint32Array.BYTES_PER_ELEMENT * index, border);
  }
  const shortestPeriod = characters.length - border;
  return shortestPeriod <= characters.length / 2;
}

function followsSequence(text: string): boolean {
  const backwards = codePoints(text).toReversed().join('');
  return SEQUENCES.some((sequence) => sequence.includes(text) || sequence.includes(backwards));
}
