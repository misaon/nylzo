import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';

import Tooltip from './Tooltip.svelte';

describe('tooltip on the server', () => {
  it('renders its label in a button described by the tooltip', () => {
    const { body } = render(Tooltip, { props: { label: 'Label', title: 'Title', text: 'Text' } });
    const [, describedBy] = /aria-describedby="([^"]+)"/u.exec(body) ?? [];

    expect(describedBy).toBeDefined();
    expect(body).toContain(`role="tooltip" id="${describedBy}"`);
    expect(body).toMatch(/<button[^>]*>[\s\S]*Label[\s\S]*<\/button>/u);
  });
});
