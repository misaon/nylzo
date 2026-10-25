import type { Environment } from 'vitest/runtime';

class FakeMediaQueryList extends EventTarget {
  matches = false;
  readonly media: string;

  constructor(media: string) {
    super();
    this.media = media;
  }
}

class FakeNode extends EventTarget {}

class FakeElement extends FakeNode {
  matches(): boolean {
    return false;
  }
}

export default {
  name: 'svelte-client',
  viteEnvironment: 'client',
  setup(global: typeof globalThis) {
    const queries = new Map<string, FakeMediaQueryList>();
    const matchMedia = (media: string): FakeMediaQueryList => {
      const query = queries.get(media) ?? new FakeMediaQueryList(media);
      queries.set(media, query);
      return query;
    };
    Object.assign(global, { window: { matchMedia }, matchMedia, Node: FakeNode, Element: FakeElement });
    return {
      teardown() {
        Reflect.deleteProperty(global, 'window');
        Reflect.deleteProperty(global, 'matchMedia');
        Reflect.deleteProperty(global, 'Node');
        Reflect.deleteProperty(global, 'Element');
      },
    };
  },
} satisfies Environment;
