import { MAX_JSON_DEPTH } from '../limits.ts';

export type JsonValue =
  | { readonly kind: 'null' }
  | { readonly kind: 'boolean'; readonly value: boolean }
  | { readonly kind: 'number'; readonly literal: string }
  | { readonly kind: 'string'; readonly value: string }
  | { readonly kind: 'array'; readonly items: readonly JsonValue[] }
  | { readonly kind: 'object'; readonly members: ReadonlyMap<string, JsonValue> };

const WHITESPACE = new Set([' ', '\t', '\n', '\r']);
const NUMBER_LITERAL = /-?(?:0|[1-9]\d*)(?:\.\d+)?(?:e[+-]?\d+)?/iy;
const INTEGER_LITERAL = /^-?(?:0|[1-9]\d*)$/;
const FOUR_HEX_DIGITS = /^[\da-f]{4}$/i;
const REPLACEMENT_CHARACTER = '�';
const LITERALS: ReadonlyMap<string, JsonValue> = new Map([
  ['true', { kind: 'boolean', value: true }],
  ['false', { kind: 'boolean', value: false }],
  ['null', { kind: 'null' }],
]);
const SIMPLE_ESCAPES: ReadonlyMap<string, string> = new Map([
  ['"', '"'],
  ['\\', '\\'],
  ['/', '/'],
  ['b', '\b'],
  ['f', '\f'],
  ['n', '\n'],
  ['r', '\r'],
  ['t', '\t'],
]);

export function parseJson(text: string): JsonValue {
  return new JsonReader(text).readDocument();
}

export function isIntegerLiteral(literal: string): boolean {
  return INTEGER_LITERAL.test(literal);
}

class JsonReader {
  readonly #text: string;
  #position = 0;

  constructor(text: string) {
    this.#text = text;
  }

  #readValue(depth: number): JsonValue {
    this.#skipWhitespace();
    const next = this.#text[this.#position];
    if (next === '{' || next === '[') {
      if (depth > MAX_JSON_DEPTH) {
        throw this.#error();
      }
      return next === '{' ? this.#readObject(depth) : this.#readArray(depth);
    }
    if (next === '"') {
      return { kind: 'string', value: this.#readString() };
    }
    for (const [literal, value] of LITERALS) {
      if (this.#text.startsWith(literal, this.#position)) {
        this.#position += literal.length;
        return value;
      }
    }
    return { kind: 'number', literal: this.#readNumber() };
  }

  #readObject(depth: number): JsonValue {
    const members = new Map<string, JsonValue>();
    this.#readSequence('}', () => {
      this.#skipWhitespace();
      if (this.#text[this.#position] !== '"') {
        throw this.#error();
      }
      const key = this.#readString();
      this.#skipWhitespace();
      this.#expect(':');
      members.set(key, this.#readValue(depth + 1));
    });
    return { kind: 'object', members };
  }

  #readArray(depth: number): JsonValue {
    const items: JsonValue[] = [];
    this.#readSequence(']', () => {
      items.push(this.#readValue(depth + 1));
    });
    return { kind: 'array', items };
  }

  #readSequence(close: string, readElement: () => void): void {
    this.#position += 1;
    this.#skipWhitespace();
    if (this.#consume(close)) {
      return;
    }
    for (;;) {
      readElement();
      this.#skipWhitespace();
      if (this.#consume(close)) {
        return;
      }
      this.#expect(',');
    }
  }

  #readString(): string {
    this.#position += 1;
    let value = '';
    for (;;) {
      const start = this.#position;
      while (isPlainStringCharacter(this.#text[this.#position])) {
        this.#position += 1;
      }
      value += this.#text.slice(start, this.#position);
      if (this.#consume('"')) {
        return value;
      }
      this.#expect('\\');
      value += this.#readEscape();
    }
  }

  #readEscape(): string {
    const marker = this.#text[this.#position];
    this.#position += 1;
    if (marker === 'u') {
      return this.#readUnicodeEscape();
    }
    const replacement = marker === undefined ? undefined : SIMPLE_ESCAPES.get(marker);
    if (replacement === undefined) {
      throw this.#error();
    }
    return replacement;
  }

  #readUnicodeEscape(): string {
    const unit = String.fromCodePoint(this.#readFourHexDigits());
    if (unit.isWellFormed()) {
      return unit;
    }
    const next = this.#peekUnicodeEscape();
    if (next !== undefined && `${unit}${next}`.isWellFormed()) {
      this.#position += 6;
      return `${unit}${next}`;
    }
    return REPLACEMENT_CHARACTER;
  }

  #peekUnicodeEscape(): string | undefined {
    if (!this.#text.startsWith('\\u', this.#position)) {
      return undefined;
    }
    const hex = this.#text.slice(this.#position + 2, this.#position + 6);
    return FOUR_HEX_DIGITS.test(hex) ? String.fromCodePoint(Number.parseInt(hex, 16)) : undefined;
  }

  #readFourHexDigits(): number {
    const hex = this.#text.slice(this.#position, this.#position + 4);
    if (!FOUR_HEX_DIGITS.test(hex)) {
      throw this.#error();
    }
    this.#position += 4;
    return Number.parseInt(hex, 16);
  }

  #readNumber(): string {
    NUMBER_LITERAL.lastIndex = this.#position;
    const match = NUMBER_LITERAL.exec(this.#text);
    if (match === null) {
      throw this.#error();
    }
    this.#position += match[0].length;
    return match[0];
  }

  #consume(character: string): boolean {
    if (this.#text[this.#position] !== character) {
      return false;
    }
    this.#position += 1;
    return true;
  }

  #expect(character: string): void {
    if (!this.#consume(character)) {
      throw this.#error();
    }
  }

  #skipWhitespace(): void {
    while (WHITESPACE.has(this.#text[this.#position] ?? '')) {
      this.#position += 1;
    }
  }

  #error(): SyntaxError {
    return new SyntaxError(`invalid JSON at offset ${this.#position}`);
  }

  readDocument(): JsonValue {
    const value = this.#readValue(1);
    this.#skipWhitespace();
    if (this.#position !== this.#text.length) {
      throw this.#error();
    }
    return value;
  }
}

function isPlainStringCharacter(character: string | undefined): boolean {
  return character !== undefined && character >= ' ' && character !== '"' && character !== '\\';
}
