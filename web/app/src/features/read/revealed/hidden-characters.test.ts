import { describe, expect, it } from 'vitest';

import { markHiddenCharacters } from './hidden-characters.ts';

describe('hidden characters in revealed text (W2-12)', () => {
  it.each([
    ['a bidi override', 'pay \u{202E}0001 tnuocca', 'pay ⟨U+202E⟩0001 tnuocca'],
    ['a zero-width space', 'pass\u{200B}word', 'pass⟨U+200B⟩word'],
    ['a lone carriage return', 'safe\rrm -rf', 'safe⟨U+000D⟩rm -rf'],
    ['a tag character after an emoji', '😀\u{E0041}\u{E0042}', '😀⟨U+E0041⟩⟨U+E0042⟩'],
    ['a Hangul filler', 'name\u{3164}', 'name⟨U+3164⟩'],
  ])('marks %s', (_, text, shown) => {
    const marked = markHiddenCharacters(text);
    expect(marked.shown).toBe(shown);
    expect(marked.hidden).toBeGreaterThan(0);
    expect(marked.pieces.map((piece) => (piece.hidden ? '' : piece.text)).join('')).not.toMatch(
      /[\u{200B}\u{202E}\r\u{3164}\u{E0041}]/u,
    );
  });

  it.each([
    ['plain text with line breaks and tabs', 'Ahoj,\n\theslo je\r\nkůň'],
    ['an emoji family joined by zero-width joiners', '👨‍👩‍👧 ❤️'],
    ['a subdivision flag made of tag characters', '🏴󠁧󠁢󠁳󠁣󠁴󠁿'],
  ])('leaves %s alone', (_, text) => {
    expect(markHiddenCharacters(text)).toEqual({ pieces: [{ key: 0, text, hidden: false }], hidden: 0, shown: text });
  });
});
