import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import { fileExtension, sanitizeFileName, splitFileName } from './file-name.ts';

type Part = readonly [text: string, count: number];

interface FileNameVector {
  readonly name: string;
  readonly input: readonly Part[];
  readonly sanitized: readonly Part[];
}

function isPart(value: unknown): value is Part {
  return Array.isArray(value) && value.length === 2 && typeof value[0] === 'string' && typeof value[1] === 'number';
}

function isParts(value: unknown): value is readonly Part[] {
  return Array.isArray(value) && value.every((part: unknown) => isPart(part));
}

function isFileNameVector(value: unknown): value is FileNameVector {
  return (
    typeof value === 'object' &&
    value !== null &&
    'name' in value &&
    typeof value.name === 'string' &&
    'input' in value &&
    isParts(value.input) &&
    'sanitized' in value &&
    isParts(value.sanitized)
  );
}

function vectorsFrom(json: string): readonly FileNameVector[] {
  const vectors: unknown = JSON.parse(json);
  if (
    typeof vectors !== 'object' ||
    vectors === null ||
    !('cases' in vectors) ||
    !Array.isArray(vectors.cases) ||
    !vectors.cases.every((vector: unknown) => isFileNameVector(vector))
  ) {
    throw new Error('spec/file-name-vectors.json has malformed cases');
  }
  return vectors.cases;
}

const cases = vectorsFrom(readFileSync(new URL('../../../../../spec/file-name-vectors.json', import.meta.url), 'utf8'));

function joinParts(parts: readonly Part[]): string {
  return parts.map(([text, count]) => text.repeat(count)).join('');
}

describe('file names', () => {
  it.each([
    ['report.pdf', 'report.pdf'],
    ['../../etc/passwd', '_.._etc_passwd'],
    [String.raw`C:\Windows\evil.exe`, 'C__Windows_evil.exe'],
    ['invoice\u{202E}fdp.exe', 'invoicefdp.exe'],
    ['bidi\u{2066}isolate\u{2069}.txt', 'bidiisolate.txt'],
    ['zero\u{200B}width.txt', 'zerowidth.txt'],
    ['soft\u{AD}hyphen.txt', 'softhyphen.txt'],
    ['line\u{2028}separator\u{2029}.txt', 'lineseparator.txt'],
    ['hangul\u{3164}filler\u{115F}.txt', 'hangulfiller.txt'],
    ['tab\tand\nnewline.txt', 'tabandnewline.txt'],
    ['  .hidden  ', 'hidden'],
    ['trailing dot.', 'trailing dot'],
    ['many   spaces.txt', 'many spaces.txt'],
    ['CON.txt', '_CON.txt'],
    ['con .txt', '_con .txt'],
    ['com¹.txt', '_com¹.txt'],
    ['LPT³', '_LPT³'],
    ['CONIN$', '_CONIN$'],
    ['conout$.log', '_conout$.log'],
    ['console.txt', 'console.txt'],
    ['com10.txt', 'com10.txt'],
    ['', 'nylzo'],
    ['...', 'nylzo'],
    ['\u{202E}', 'nylzo'],
    ['cafe\u{301}.txt', 'caf\u{E9}.txt'],
  ])('sanitises %j to %j', (input, expected) => {
    expect(sanitizeFileName(input)).toBe(expected);
  });

  it('splits a name at its last dot only when the extension is short', () => {
    expect(splitFileName('report.final.pdf')).toEqual({ stem: 'report.final', extension: '.pdf' });
    expect(splitFileName('.bashrc')).toEqual({ stem: '.bashrc', extension: '' });
    expect(splitFileName('archive')).toEqual({ stem: 'archive', extension: '' });
    expect(splitFileName(`a.${'x'.repeat(16)}`)).toEqual({ stem: 'a', extension: `.${'x'.repeat(16)}` });
    expect(splitFileName(`a.${'x'.repeat(17)}`)).toEqual({ stem: `a.${'x'.repeat(17)}`, extension: '' });
  });
});

describe('file names match the CLI (spec/file-name-vectors.json)', () => {
  it('has vectors', () => {
    expect(cases.length).toBeGreaterThan(0);
  });
  it.each(cases)('$name', ({ input, sanitized }) => {
    expect(sanitizeFileName(joinParts(input))).toBe(joinParts(sanitized));
  });
});

describe('file extensions', () => {
  it('reports the extension that the sanitised name really has', () => {
    expect(fileExtension('invoice\u{202E}fdp.exe')).toBe('exe');
    expect(fileExtension('Report.PDF')).toBe('pdf');
    expect(fileExtension('.bashrc')).toBe('');
    expect(fileExtension('archive')).toBe('');
  });
});
