interface TextPiece {
  readonly key: number;
  readonly text: string;
  readonly hidden: boolean;
}

export interface MarkedText {
  readonly pieces: readonly TextPiece[];
  readonly hidden: number;
  readonly shown: string;
}

const SUSPECT = /[\p{Cc}\p{Cf}\p{Default_Ignorable_Code_Point}\u{2028}\u{2029}]/u;
const PICTOGRAPHIC = /^[\p{Extended_Pictographic}\p{Regional_Indicator}]/u;
const EMOJI_JOINERS = new Set([0x20_0d, 0xfe_0e, 0xfe_0f]);
const BLACK_FLAG = 0x1_f3_f4;
const TAG_FIRST = 0xe_00_20;
const TAG_CANCEL = 0xe_00_7f;
const LINE_BREAKS = new Set(['\n', '\t', '\r\n']);

const segmenter = new Intl.Segmenter(undefined, { granularity: 'grapheme' });

function codeLabel(codePoint: number): string {
  return `U+${codePoint.toString(16).toUpperCase().padStart(4, '0')}`;
}

function isFlagTag(codePoints: readonly number[], index: number): boolean {
  const codePoint = codePoints[index] ?? 0;
  const terminated = codePoints.at(-1) === TAG_CANCEL;
  return codePoints[0] === BLACK_FLAG && terminated && codePoint >= TAG_FIRST && codePoint <= TAG_CANCEL;
}

function allowedInEmoji(codePoints: readonly number[], index: number): boolean {
  return EMOJI_JOINERS.has(codePoints[index] ?? 0) || isFlagTag(codePoints, index);
}

function splitGrapheme(grapheme: string, add: (text: string, hidden: boolean) => void): void {
  if (LINE_BREAKS.has(grapheme) || !SUSPECT.test(grapheme)) {
    add(grapheme, false);
    return;
  }
  const characters = grapheme.match(/./gsu) ?? [];
  const codePoints = characters.map((character) => character.codePointAt(0) ?? 0);
  const emoji = PICTOGRAPHIC.test(grapheme);
  for (const [index, character] of characters.entries()) {
    const hidden = SUSPECT.test(character) && !(emoji && allowedInEmoji(codePoints, index));
    add(hidden ? codeLabel(codePoints[index] ?? 0) : character, hidden);
  }
}

export function markHiddenCharacters(text: string): MarkedText {
  const pieces: TextPiece[] = [];
  let visible: string[] = [];
  const flush = () => {
    if (visible.length === 0) {
      return;
    }
    pieces.push({ key: pieces.length, text: visible.join(''), hidden: false });
    visible = [];
  };
  for (const { segment } of segmenter.segment(text)) {
    splitGrapheme(segment, (piece, hidden) => {
      if (!hidden) {
        visible.push(piece);
        return;
      }
      flush();
      pieces.push({ key: pieces.length, text: piece, hidden: true });
    });
  }
  flush();
  const hidden = pieces.filter((piece) => piece.hidden).length;
  return { pieces, hidden, shown: pieces.map((piece) => (piece.hidden ? `⟨${piece.text}⟩` : piece.text)).join('') };
}
