import type { MessageKey } from '../../../../shared/language/czech.ts';

interface LinkPart {
  readonly id: string;
  readonly text: string;
  readonly title: MessageKey;
  readonly meta: MessageKey;
  readonly body: MessageKey;
  readonly tone: string;
  readonly bar: string;
  readonly glow: string;
  readonly delays: { readonly text: string; readonly bar: string; readonly title: string; readonly meta: string };
}

export const LINK_PARTS: readonly LinkPart[] = [
  {
    id: 'host',
    text: 'https://nylzo.example/r',
    title: 'how.link.host.title',
    meta: 'how.link.host.meta',
    body: 'how.link.host.body',
    tone: 'text-ink-soft',
    bar: 'bg-ink-soft/35',
    glow: 'shadow-[0_0_8px_oklch(0.86_0.014_255/0.6)]',
    delays: {
      text: '[--reveal-delay:320ms]',
      bar: '[--reveal-delay:400ms]',
      title: '[--reveal-delay:480ms]',
      meta: '[--reveal-delay:540ms]',
    },
  },
  {
    id: 'meta',
    text: '#2.0.',
    title: 'how.link.meta.title',
    meta: 'how.link.meta.meta',
    body: 'how.link.meta.body',
    tone: 'text-solar-300',
    bar: 'bg-solar-300/35',
    glow: 'shadow-[0_0_8px_oklch(0.86_0.12_85/0.7)]',
    delays: {
      text: '[--reveal-delay:470ms]',
      bar: '[--reveal-delay:550ms]',
      title: '[--reveal-delay:630ms]',
      meta: '[--reveal-delay:690ms]',
    },
  },
  {
    id: 'id',
    text: 'Q3tN0xvR8kLp2WcY5aHf1g',
    title: 'how.link.id.title',
    meta: 'how.link.id.meta',
    body: 'how.link.id.body',
    tone: 'text-rose-300',
    bar: 'bg-rose-400/35',
    glow: 'shadow-[0_0_8px_oklch(0.8_0.08_350/0.8)]',
    delays: {
      text: '[--reveal-delay:620ms]',
      bar: '[--reveal-delay:700ms]',
      title: '[--reveal-delay:780ms]',
      meta: '[--reveal-delay:840ms]',
    },
  },
  {
    id: 'key',
    text: '.Vh2sXq9LmB4tRk7PzW1eNc6YuJ3oFa8DiG5bTn0KwEs.T7cQm…',
    title: 'how.link.key.title',
    meta: 'how.link.key.meta',
    body: 'how.link.key.body',
    tone: 'text-aegis-300',
    bar: 'bg-aegis-300/35',
    glow: 'shadow-glow',
    delays: {
      text: '[--reveal-delay:770ms]',
      bar: '[--reveal-delay:850ms]',
      title: '[--reveal-delay:930ms]',
      meta: '[--reveal-delay:990ms]',
    },
  },
];
