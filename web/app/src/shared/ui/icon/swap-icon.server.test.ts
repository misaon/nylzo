import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';

import SwapIcon from './SwapIcon.svelte';

describe('swap icon on the server', () => {
  it('renders both icons at the default size with the first one shown', () => {
    const { body } = render(SwapIcon, { props: { from: 'copy', to: 'check', swapped: false } });
    const classes = [...body.matchAll(/<svg class="([^"]*)"/gu)].map(([, value]) => value ?? '');

    expect(classes).toHaveLength(2);
    expect(classes.every((value) => value.split(' ').includes('size-4'))).toBe(true);
    expect(classes[0]?.split(' ')).toContain('opacity-100');
    expect(classes[1]?.split(' ')).toContain('opacity-0');
  });
});
