import { render } from 'svelte/server';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { formatBytes, formatSize } from '../../../shared/language/format.ts';
import { getTranslator, Translator } from '../../../shared/language/translator.svelte.ts';
import { getScreenWork, ScreenWork } from '../../../shared/navigation/screen-work.ts';
import { getSessionSentStore, sessionSentStore } from '../../../shared/sent-records/session-sent-store.ts';
import { Composer } from './composer.svelte.ts';
import ComposerNotes from './ComposerNotes.svelte';
import ComposerSection from './ComposerSection.svelte';
import MessageField from './MessageField.svelte';

interface ServerState {
  left: boolean;
  sealed: object | undefined;
  placeholder: object | undefined;
}

const serverState = vi.hoisted<ServerState>(() => ({ left: false, sealed: undefined, placeholder: undefined }));

vi.mock(import('./composer.svelte.ts'), (original) =>
  original().then((actual) => ({
    ...actual,
    Composer: class extends actual.Composer {
      constructor(...arguments_: ConstructorParameters<typeof actual.Composer>) {
        super(...arguments_);
        Object.defineProperties(this, {
          left: { get: () => serverState.left },
          sealed: { get: () => serverState.sealed },
          placeholder: { get: () => serverState.placeholder },
        });
      }
    },
  })),
);
vi.mock('../created/CreatedPanel.svelte', () => ({
  default: (renderer: { push: (html: string) => void }, { sizing = false }: { readonly sizing?: boolean }) => {
    renderer.push(sizing ? '<i data-panel="placeholder"></i>' : '<i data-panel="created"></i>');
  },
}));
vi.mock(import('../../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);
vi.mock(import('../../../shared/navigation/screen-work.ts'), (original) =>
  original().then((actual) => ({ ...actual, getScreenWork: vi.fn<typeof actual.getScreenWork>() })),
);
vi.mock(import('../../../shared/sent-records/session-sent-store.ts'), (original) =>
  original().then((actual) => ({ ...actual, getSessionSentStore: vi.fn<typeof actual.getSessionSentStore>() })),
);

describe('the composer rendered on the server', () => {
  const translator = new Translator('en');

  beforeEach(() => {
    vi.mocked(getTranslator).mockReturnValue(translator);
    vi.mocked(getScreenWork).mockReturnValue(new ScreenWork());
    vi.mocked(getSessionSentStore).mockReturnValue(sessionSentStore());
    Object.assign(serverState, { left: false, sealed: undefined, placeholder: undefined });
  });

  it.each([
    { name: 'a sealed message', state: { sealed: { created: {}, opensMax: 1, passphrase: '' } }, panel: 'created' },
    { name: 'a message being sealed', state: { placeholder: {} }, panel: 'placeholder' },
    { name: 'a page that was left', state: { left: true, sealed: {} }, panel: undefined },
    { name: 'an empty draft', state: {}, panel: undefined },
  ])('renders the panel that belongs to $name', ({ state, panel }) => {
    Object.assign(serverState, state);
    const { body } = render(ComposerSection);
    expect(/data-panel="(\w+)"/u.exec(body)?.[1]).toBe(panel);
  });

  it('shows the counter of a draft close to its limit', () => {
    const composer = new Composer(translator, sessionSentStore());
    const field = () => render(MessageField, { props: { composer, noticeId: 'notice' } }).body;
    const counter = () =>
      translator.translate('compose.counter', {
        size: formatSize(composer.textBytes, translator.language),
        max: formatBytes(composer.textLimit, translator.language),
      });
    expect(field()).not.toContain(counter());
    composer.text = 'a'.repeat(composer.textLimit);
    expect(field()).toContain(counter());
  });

  it('offers to load the limits again once they failed to load', () => {
    const composer = new Composer(translator, sessionSentStore());
    const retry = translator.translate('action.retry');
    const notes = () => render(ComposerNotes, { props: { composer, noticeId: 'notice' } }).body;
    expect(notes()).not.toContain(retry);
    composer.loadFailed = true;
    expect(notes()).toContain(retry);
  });
});
