import { describe, expect, it } from 'vitest';

import { isRiskyFile, sanitizeFileName } from '../src/lib/download.ts';

describe('file names', () => {
  it.each([
    ['report.pdf', 'report.pdf'],
    ['../../etc/passwd', '_.._etc_passwd'],
    ['C:\\Windows\\evil.exe', 'C__Windows_evil.exe'],
    ['invoice\u202egpj.exe', 'invoicegpj.exe'],
    ['zero\u200bwidth.txt', 'zerowidth.txt'],
    ['tab\tand\nnewline.txt', 'tabandnewline.txt'],
    ['  .hidden  ', 'hidden'],
    ['trailing dot.', 'trailing dot'],
    ['CON.txt', '_CON.txt'],
    ['', 'nylzo'],
    ['...', 'nylzo'],
    ['cafe\u0301.txt', 'caf\u00e9.txt'],
  ])('sanitises %j to %j', (input, expected) => {
    expect(sanitizeFileName(input)).toBe(expected);
  });

  it('keeps the extension when it cuts a long name to 255 bytes', () => {
    const name = sanitizeFileName(`${'č'.repeat(200)}.tar.gz`);
    expect(new TextEncoder().encode(name).length).toBeLessThanOrEqual(255);
    expect(name.endsWith('.gz')).toBe(true);
  });

  it('flags executable and active content', () => {
    expect(isRiskyFile('setup.EXE')).toBe(true);
    expect(isRiskyFile('page.html')).toBe(true);
    expect(isRiskyFile('image.svg')).toBe(true);
    expect(isRiskyFile('notes.txt')).toBe(false);
    expect(isRiskyFile('archive')).toBe(false);
  });
});
