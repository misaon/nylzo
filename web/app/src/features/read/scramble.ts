import { randomText } from '../../shared/pipeline/random-text.ts';

const SCRAMBLE_LEAD = 1.15;
const WHITESPACE: ReadonlySet<string> = new Set([' ', '\n', '\t']);

export function scramble(characters: readonly string[], progress: number): string {
  const cut = Math.floor(characters.length * Math.min(1, progress * SCRAMBLE_LEAD));
  const noise = randomText(characters.length);
  return characters
    .map((character, index) => (index < cut || WHITESPACE.has(character) ? character : noise.charAt(index)))
    .join('');
}

export function charactersOf(text: string): readonly string[] {
  return Array.from(text.matchAll(/./gsu), ([character]) => character);
}
