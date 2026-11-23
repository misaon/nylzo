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
const EMOJI_JOINERS = new Set(['\u{200D}', '\u{FE0E}', '\u{FE0F}']);
const BLACK_FLAG = '\u{1F3F4}';
const TAG_CANCEL = '\u{E007F}';
const TAG = /^[\u{E0020}-\u{E007F}]$/u;
const LINE_BREAKS = new Set(['\n', '\t', '\r\n']);

const segmenter = new Intl.Segmenter(undefined, { granularity: 'grapheme' });

function codeLabel(character: string): string {
  return `U+${Number(character.codePointAt(0)).toString(16).toUpperCase().padStart(4, '0')}`;
}

function isFlagTag(characters: readonly string[], character: string): boolean {
  return characters[0] === BLACK_FLAG && characters.at(-1) === TAG_CANCEL && TAG.test(character);
}

function allowedInEmoji(characters: readonly string[], character: string): boolean {
  return EMOJI_JOINERS.has(character) || isFlagTag(characters, character);
}

function splitGrapheme(grapheme: string, add: (text: string, hidden: boolean) => void): void {
  if (LINE_BREAKS.has(grapheme) || !SUSPECT.test(grapheme)) {
    add(grapheme, false);
    return;
  }
  const characters = characterList(grapheme);
  const emoji = PICTOGRAPHIC.test(grapheme);
  for (const character of characters) {
    const hidden = SUSPECT.test(character) && !(emoji && allowedInEmoji(characters, character));
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
    pieces.push({ key: pieces.length, text: codeLabel(piece), hidden: true, original: piece });
  };
  for (const { segment } of segmenter.segment(text)) {
    splitGrapheme(segment, appendPiece);
  }
  flush();
  const hidden = pieces.filter((piece) => piece.hidden).length;
  return { pieces, hidden, shown: pieces.map((piece) => shownText(piece)).join('') };
}

function characterList(text: string): readonly string[] {
  return Array.from(text.matchAll(/./gsu), ([character]) => character);
}

function shownText(piece: TextPiece): string {
  return piece.hidden ? `⟨${piece.text}⟩` : piece.text;
}

interface PieceSpan {
  readonly piece: TextPiece;
  readonly index: number;
  readonly start: number;
  readonly end: number;
}

const PIECE_SPANS = new WeakMap<MarkedText, readonly PieceSpan[]>();

function pieceSpans(marked: MarkedText): readonly PieceSpan[] {
  let spans = PIECE_SPANS.get(marked);
  if (spans === undefined) {
    let total = 0;
    spans = marked.pieces.map((piece, index) => {
      const start = total;
      total += characterList(shownText(piece)).length;
      return { piece, index, start, end: total };
    });
    PIECE_SPANS.set(marked, spans);
  }
  return spans;
}

// The scramble keeps every character of the head in place and leaves the rest alone, so only head pieces change.
export function piecesOf(head: string, marked: MarkedText): readonly TextPiece[] {
  const scrambled = characterList(head);
  const spans = pieceSpans(marked);
  if (scrambled.length > (spans.at(-1)?.end ?? 0)) {
    return marked.pieces;
  }
  const pieces: TextPiece[] = [];
  for (const { piece, index, start, end } of spans) {
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
        {
          key: -1 - piece.key,
          text: characterList(piece.text)
            .slice(scrambled.length - start)
            .join(''),
          hidden: false,
        },
      );
    }
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
        mark.replaceWith(touched ? String(mark.dataset['original']) : '');
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
