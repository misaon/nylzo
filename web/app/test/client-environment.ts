import type { Environment } from 'vitest/runtime';

class FakeMediaQueryList extends EventTarget {
  matches = false;
  readonly media: string;

  constructor(media: string) {
    super();
    this.media = media;
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
    Object.assign(global, { window: { matchMedia }, Node: class FakeNode extends EventTarget {} });
    return {
      teardown() {
        Reflect.deleteProperty(global, 'window');
        Reflect.deleteProperty(global, 'Node');
      },
    };
  },
} satisfies Environment;
