import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';

import StateCard from './StateCard.svelte';

describe('state card on the server', () => {
  it('renders its content when nothing is loading', () => {
    const children = createRawSnippet(() => ({ render: () => '<p data-content>content</p>' }));

    const { body } = render(StateCard, { props: { children } });

    expect(body).toContain('<p data-content>');
    expect(body).not.toContain('role="status"');
  });
});
