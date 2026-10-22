import { MAX_JSON_DEPTH } from './limits.ts';

export type JsonValue =
  | { readonly kind: 'null' }
  | { readonly kind: 'boolean'; readonly value: boolean }
  | { readonly kind: 'number'; readonly literal: string }
  | { readonly kind: 'string'; readonly value: string }
  | { readonly kind: 'array'; readonly items: readonly JsonValue[] }
  | { readonly kind: 'object'; readonly members: ReadonlyMap<string, JsonValue> };

const QUOTE = 0x22;
const BACKSLASH = 0x5c;
const WHITESPACE = new Set([0x20, 0x09, 0x0a, 0x0d]);
const NUMBER_LITERAL = /-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/y;
const INTEGER_LITERAL = /^-?(?:0|[1-9][0-9]*)$/;
const HEX4 = /^[0-9a-fA-F]{4}$/;
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
  #pos = 0;

  constructor(text: string) {
    this.#text = text;
  }

  readDocument(): JsonValue {
    const value = this.readValue(1);
    this.skipWhitespace();
    if (this.#pos !== this.#text.length) {
      throw this.error();
    }
    return value;
  }

  private readValue(depth: number): JsonValue {
    this.skipWhitespace();
    const next = this.#text[this.#pos];
    if ((next === '{' || next === '[') && depth > MAX_JSON_DEPTH) {
      throw this.error();
    }
    switch (next) {
      case '{':
        return this.readObject(depth);
      case '[':
        return this.readArray(depth);
      case '"':
        return { kind: 'string', value: this.readString() };
      case 't':
        this.readLiteral('true');
        return { kind: 'boolean', value: true };
      case 'f':
        this.readLiteral('false');
        return { kind: 'boolean', value: false };
      case 'n':
        this.readLiteral('null');
        return { kind: 'null' };
      default:
        return { kind: 'number', literal: this.readNumber() };
    }
  }

  private readObject(depth: number): JsonValue {
    this.#pos += 1;
    const members = new Map<string, JsonValue>();
    this.skipWhitespace();
    if (this.consume('}')) {
      return { kind: 'object', members };
    }
    for (;;) {
      this.skipWhitespace();
      if (this.#text[this.#pos] !== '"') {
        throw this.error();
      }
      const key = this.readString();
      this.skipWhitespace();
      if (!this.consume(':')) {
        throw this.error();
      }
      members.set(key, this.readValue(depth + 1));
      this.skipWhitespace();
      if (this.consume('}')) {
        return { kind: 'object', members };
      }
      if (!this.consume(',')) {
        throw this.error();
      }
    }
  }

  private readArray(depth: number): JsonValue {
    this.#pos += 1;
    const items: JsonValue[] = [];
    this.skipWhitespace();
    if (this.consume(']')) {
      return { kind: 'array', items };
    }
    for (;;) {
      items.push(this.readValue(depth + 1));
      this.skipWhitespace();
      if (this.consume(']')) {
        return { kind: 'array', items };
      }
      if (!this.consume(',')) {
        throw this.error();
      }
    }
  }

  private readString(): string {
    this.#pos += 1;
    let out = '';
    for (;;) {
      const start = this.#pos;
      while (this.#pos < this.#text.length && isPlainStringChar(this.#text.charCodeAt(this.#pos))) {
        this.#pos += 1;
      }
      out += this.#text.slice(start, this.#pos);
      const code = this.#text.charCodeAt(this.#pos);
      if (Number.isNaN(code) || code < 0x20) {
        throw this.error();
      }
      this.#pos += 1;
      if (code === QUOTE) {
        return out;
      }
      out += this.readEscape();
    }
  }

  private readEscape(): string {
    const marker = this.#text[this.#pos];
    this.#pos += 1;
    if (marker === 'u') {
      return this.readUnicodeEscape();
    }
    const replacement = marker === undefined ? undefined : SIMPLE_ESCAPES.get(marker);
    if (replacement === undefined) {
      throw this.error();
    }
    return replacement;
  }

  private readUnicodeEscape(): string {
    const unit = this.readHex4();
    if (!isSurrogate(unit)) {
      return String.fromCharCode(unit);
    }
    const next = this.peekUnicodeEscape();
    if (isHighSurrogate(unit) && next !== undefined && isLowSurrogate(next)) {
      this.#pos += 6;
      return String.fromCharCode(unit, next);
    }
    return '�';
  }

  private peekUnicodeEscape(): number | undefined {
    if (!this.#text.startsWith('\\u', this.#pos)) {
      return undefined;
    }
    const hex = this.#text.slice(this.#pos + 2, this.#pos + 6);
    return HEX4.test(hex) ? parseInt(hex, 16) : undefined;
  }

  private readHex4(): number {
    const hex = this.#text.slice(this.#pos, this.#pos + 4);
    if (!HEX4.test(hex)) {
      throw this.error();
    }
    this.#pos += 4;
    return parseInt(hex, 16);
  }

  private readNumber(): string {
    NUMBER_LITERAL.lastIndex = this.#pos;
    const match = NUMBER_LITERAL.exec(this.#text);
    if (match === null) {
      throw this.error();
    }
    this.#pos += match[0].length;
    return match[0];
  }

  private readLiteral(literal: string): void {
    if (!this.#text.startsWith(literal, this.#pos)) {
      throw this.error();
    }
    this.#pos += literal.length;
  }

  private consume(char: string): boolean {
    if (this.#text[this.#pos] !== char) {
      return false;
    }
    this.#pos += 1;
    return true;
  }

  private skipWhitespace(): void {
    while (WHITESPACE.has(this.#text.charCodeAt(this.#pos))) {
      this.#pos += 1;
    }
  }

  private error(): SyntaxError {
    return new SyntaxError(`invalid JSON at offset ${this.#pos}`);
  }
}

function isPlainStringChar(code: number): boolean {
  return code >= 0x20 && code !== QUOTE && code !== BACKSLASH;
}

function isSurrogate(unit: number): boolean {
  return unit >= 0xd800 && unit <= 0xdfff;
}

function isHighSurrogate(unit: number): boolean {
  return unit >= 0xd800 && unit <= 0xdbff;
}

function isLowSurrogate(unit: number): boolean {
  return unit >= 0xdc00 && unit <= 0xdfff;
}
