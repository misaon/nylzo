import { createRawSnippet, flushSync, mount, unmount } from 'svelte';
import { afterEach, describe, expect, it } from 'vitest';

import StateCard from './StateCard.svelte';

describe('state card', () => {
  let component: ReturnType<typeof mount> | undefined;
  const content = createRawSnippet(() => ({ render: () => '<p data-content>content</p>' }));

  afterEach(async () => {
    if (component) {
      await unmount(component);
    }
    document.body.replaceChildren();
  });

  function show(props: { readonly loading?: string }): HTMLElement {
    const target = document.createElement('div');
    document.body.append(target);
    component = mount(StateCard, { target, props: { ...props, children: content } });
    flushSync();
    return target;
  }

  it('shows its content when nothing is loading', () => {
    const target = show({});

    expect(target.querySelector('[data-content]')).not.toBeNull();
    expect(target.querySelector('[role="status"]')).toBeNull();
  });

  it('announces what is loading in place of its content', () => {
    const target = show({ loading: 'Loading' });

    expect(target.querySelector('[role="status"]')?.textContent).toBe('Loading');
    expect(target.querySelector('[data-content]')).toBeNull();
  });
});
