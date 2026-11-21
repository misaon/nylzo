import type { Attachment } from 'svelte/attachments';

type TextPiece =
  | { readonly key: number; readonly text: string; readonly hidden: false }
  | { readonly key: number; readonly text: string; readonly hidden: true; readonly original: string };

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
    add(character, hidden);
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
  const appendPiece = (piece: string, hidden: boolean): void => {
    if (!hidden) {
      visible.push(piece);
      return;
    }
    flush();
    pieces.push({ key: pieces.length, text: codeLabel(piece.codePointAt(0) ?? 0), hidden: true, original: piece });
  };
  for (const { segment } of segmenter.segment(text)) {
    splitGrapheme(segment, appendPiece);
  }
  flush();
  const hidden = pieces.filter((piece) => piece.hidden).length;
  return { pieces, hidden, shown: pieces.map((piece) => shownText(piece)).join('') };
}

function characterList(text: string): readonly string[] {
  return text.match(/./gsu) ?? [];
}

function shownText(piece: TextPiece): string {
  return piece.hidden ? `⟨${piece.text}⟩` : piece.text;
}

const PIECE_ENDS = new WeakMap<MarkedText, readonly number[]>();

function pieceEnds(marked: MarkedText): readonly number[] {
  let ends = PIECE_ENDS.get(marked);
  if (ends === undefined) {
    let total = 0;
    ends = marked.pieces.map((piece) => (total += characterList(shownText(piece)).length));
    PIECE_ENDS.set(marked, ends);
  }
  return ends;
}

function afterCharacters(text: string, count: number): string {
  let offset = 0;
  for (let index = 0; index < count; index += 1) {
    offset += (text.codePointAt(offset) ?? 0) > 0xff_ff ? 2 : 1;
  }
  return text.slice(offset);
}

// The scramble keeps every character of the head in place and leaves the rest alone, so only head pieces change.
export function piecesOf(head: string, marked: MarkedText): readonly TextPiece[] {
  const scrambled = characterList(head);
  const ends = pieceEnds(marked);
  if (scrambled.length > (ends.at(-1) ?? 0)) {
    return marked.pieces;
  }
  const pieces: TextPiece[] = [];
  let start = 0;
  for (const [index, piece] of marked.pieces.entries()) {
    const end = ends[index] ?? start;
    if (start >= scrambled.length) {
      return [...pieces, ...marked.pieces.slice(index)];
    }
    if (piece.hidden) {
      pieces.push(piece);
    } else if (end <= scrambled.length) {
      pieces.push({ ...piece, text: scrambled.slice(start, end).join('') });
    } else {
      pieces.push(
        { ...piece, text: scrambled.slice(start).join('') },
        { key: -1 - piece.key, text: afterCharacters(piece.text, scrambled.length - start), hidden: false },
      );
    }
    start = end;
  }
  return pieces;
}

function editing(): boolean {
  const active = document.activeElement;
  return (
    active instanceof HTMLInputElement ||
    active instanceof HTMLTextAreaElement ||
    (active?.closest('[contenteditable]') ?? null) !== null
  );
}

// A copy made inside the message by hand carries the hidden characters themselves, not the labels that show them.
export const copyOriginals: Attachment<HTMLElement> = (message) => {
  const listening = new AbortController();
  document.addEventListener(
    'copy',
    (event) => {
      const selection = getSelection();
      if (selection === null || selection.rangeCount !== 1 || event.clipboardData === null || editing()) {
        return;
      }
      const range = selection.getRangeAt(0);
      if (!range.intersectsNode(message)) {
        return;
      }
      // A triple click or a drag past the end also selects the space after the message, which the copy keeps out.
      const inside = range.cloneRange();
      if (!message.contains(range.startContainer)) {
        inside.setStart(message, 0);
      }
      if (!message.contains(range.endContainer)) {
        inside.setEnd(message, message.childNodes.length);
      }
      if (range.toString().trim() !== inside.toString().trim()) {
        return;
      }
      const copied = inside.cloneContents();
      let replaced = false;
      for (const mark of copied.querySelectorAll<HTMLElement>('mark[data-original]')) {
        const touched = mark.textContent !== '';
        mark.replaceWith(touched ? (mark.dataset['original'] ?? '') : '');
        replaced ||= touched;
      }
      if (replaced) {
        event.clipboardData.setData('text/plain', copied.textContent);
        event.preventDefault();
      }
    },
    { signal: listening.signal },
  );
  return () => {
    listening.abort();
  };
};
