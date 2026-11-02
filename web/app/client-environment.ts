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

class FakeIntersectionObserver {
  readonly #notify: (entries: readonly { readonly isIntersecting: boolean }[]) => void;

  constructor(notify: (entries: readonly { readonly isIntersecting: boolean }[]) => void) {
    this.#notify = notify;
  }

  observe(): void {
    this.#notify([{ isIntersecting: true }]);
  }

  disconnect(): void {
    // This synchronous observer fake owns no native observer to disconnect.
  }
}

class FakeElement extends FakeNode {
  matches(): boolean {
    return false;
  }
}

export default {
  name: 'svelte-client',
  viteEnvironment: 'client',
  // oxlint-disable-next-line typescript/prefer-readonly-parameter-types -- The test environment installs and removes browser globals.
  setup(global: typeof globalThis) {
    const queries = new Map<string, FakeMediaQueryList>();
    const matchMedia = (media: string): FakeMediaQueryList => {
      const query = queries.get(media) ?? new FakeMediaQueryList(media);
      queries.set(media, query);
      return query;
    };
    Object.assign(global, {
      window: { matchMedia },
      matchMedia,
      Node: FakeNode,
      Element: FakeElement,
      IntersectionObserver: FakeIntersectionObserver,
    });
    return {
      teardown() {
        Reflect.deleteProperty(global, 'window');
        Reflect.deleteProperty(global, 'matchMedia');
        Reflect.deleteProperty(global, 'Node');
        Reflect.deleteProperty(global, 'Element');
        Reflect.deleteProperty(global, 'IntersectionObserver');
      },
    };
  },
} satisfies Environment;
