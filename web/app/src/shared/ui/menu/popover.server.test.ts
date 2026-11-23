import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { describe, expect, it, vi } from 'vitest';

import Popover from './Popover.svelte';

vi.mock(import('svelte/reactivity'), (original) =>
  original().then((actual) => ({
    ...actual,
    MediaQuery: class extends actual.MediaQuery {
      constructor(query: string) {
        super(query, true);
      }
    },
  })),
);

describe('popover on the server', () => {
  it('renders a closed bottom sheet with its grabber on a narrow screen', () => {
    const children = createRawSnippet(() => ({ render: () => '<button>item</button>' }));

    const { body } = render(Popover, {
      props: {
        id: 'menu',
        label: 'Menu',
        open: false,
        onclose: vi.fn<() => void>(),
        ondismiss: vi.fn<() => void>(),
        children,
      },
    });

    expect(body).toContain('popover="manual"');
    expect(body).toMatch(/class="[^"]*\bbottom-sheet\b/u);
    expect(body).toContain('inert');
    expect(body).toMatch(/<span[^>]*aria-hidden="true"[^>]*><\/span>/u);
  });
});
