import { describe, expect, it } from 'vitest';

import { autofixerIsClean, forbidden } from '../scripts/forbidden.ts';

describe('source guard', () => {
  it.each([
    ['a.ts', 'node.innerHTML = text;'],
    ['a.ts', 'node.outerHTML = text;'],
    ['a.ts', "node.insertAdjacentHTML('beforeend', text);"],
    ['a.ts', 'document.write(text);'],
    ['a.ts', 'document . writeln(text);'],
    ['a.svelte', '<p>{@html text}</p>'],
    ['a.svelte', '<p style:color="red">x</p>'],
    ['a.svelte', '<p style="color: red">x</p>'],
  ])('refuses %s with %j', (path, line) => {
    expect(forbidden(path, `ok\n${line}`)).toEqual([expect.stringMatching(new RegExp(`^${path.replace('.', '\\.')}:2: `))]);
  });

  it('allows ordinary code and style: in TypeScript', () => {
    expect(forbidden('a.ts', "new Intl.NumberFormat(language, { style: 'unit' });\nnode.textContent = text;")).toEqual([]);
    expect(forbidden('a.svelte', '<p class="text-sm">{text}</p>')).toEqual([]);
  });

  it('reads the autofixer report', () => {
    expect(autofixerIsClean('{\n  issues: [],\n  suggestions: [],\n  require_another_tool_call_after_fixing: false\n}')).toBe(true);
    expect(autofixerIsClean("{\n  issues: [ '`{@html}` can lead to XSS attack.' ],\n  suggestions: [],\n}")).toBe(false);
    expect(autofixerIsClean("{\n  issues: [],\n  suggestions: [ 'use $derived' ],\n}")).toBe(false);
    expect(autofixerIsClean('')).toBe(false);
  });
});
