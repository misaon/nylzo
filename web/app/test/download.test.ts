import { describe, expect, it } from 'vitest';

import { fileExtension, isKnownSafeFile, sanitizeFileName, splitFileName } from '../src/lib/download.ts';

describe('file names', () => {
  it.each([
    ['report.pdf', 'report.pdf'],
    ['../../etc/passwd', '_.._etc_passwd'],
    ['C:\\Windows\\evil.exe', 'C__Windows_evil.exe'],
    ['invoice\u202efdp.exe', 'invoicefdp.exe'],
    ['bidi\u2066isolate\u2069.txt', 'bidiisolate.txt'],
    ['zero\u200bwidth.txt', 'zerowidth.txt'],
    ['soft\u00adhyphen.txt', 'softhyphen.txt'],
    ['line\u2028separator\u2029.txt', 'lineseparator.txt'],
    ['hangul\u3164filler\u115f.txt', 'hangulfiller.txt'],
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
    ['\u202e', 'nylzo'],
    ['cafe\u0301.txt', 'caf\u00e9.txt'],
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

  it('keeps the extension when it cuts a long name to 255 bytes', () => {
    const name = sanitizeFileName(`${'č'.repeat(200)}.tar.gz`);
    expect(new TextEncoder().encode(name).length).toBeLessThanOrEqual(255);
    expect(name.endsWith('.gz')).toBe(true);
  });
});

describe('known safe files (D35)', () => {
  it.each(['notes.txt', 'README.md', 'data.csv', 'report.PDF', 'photo.jpeg', 'scan.HEIC', 'table.xlsx', 'letter.odt'])('trusts %s', (name) => {
    expect(isKnownSafeFile(name)).toBe(true);
  });

  it.each([
    'setup.exe', 'script.ps1', 'run.pif', 'link.scf', 'console.msc', 'help.chm', 'app.application', 'types.ps1xml',
    'launch.jnlp', 'macro.docm', 'macro.xlsm', 'macro.pptm', 'disk.img', 'disk.vhd', 'disk.vhdx', 'page.mht', 'page.html',
    'drawing.svg', 'archive.zip', 'no-extension', 'invoice\u202efdp.exe', 'report.pdf.exe',
  ])('asks before %j', (name) => {
    expect(isKnownSafeFile(name)).toBe(false);
  });

  it('reports the extension that the sanitised name really has', () => {
    expect(fileExtension('invoice\u202efdp.exe')).toBe('exe');
    expect(fileExtension('Report.PDF')).toBe('pdf');
    expect(fileExtension('.bashrc')).toBe('');
    expect(fileExtension('archive')).toBe('');
  });
});
