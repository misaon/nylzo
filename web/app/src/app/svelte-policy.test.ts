import { describe, expect, it } from 'vitest';

import { sveltePolicy } from '../../scripts/svelte-policy.ts';

describe('Svelte template security policy', () => {
  it.each([
    '<div>{@html value}</div>',
    '<div bind:innerHTML={value}></div>',
    '<div style="color:red"></div>',
    '<div style:color={value}></div>',
    '<button>submit</button>',
    '<script>let value = 1;</script>',
    '{#if true}{@const value = 1}{value}{/if}',
  ])('rejects unsafe or implicit markup: %s', (source) => {
    expect(sveltePolicy(source).length).toBeGreaterThan(0);
  });
  it('accepts typed declarative markup and ignores examples inside comments', () => {
    expect(
      sveltePolicy(
        '<script lang="ts">const value = "safe";</script><!-- {@html example} --><button type="button">{value}</button>',
      ),
    ).toEqual([]);
  });
});
