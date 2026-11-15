import type { MessageKey } from '../../../../shared/language/catalog.ts';

interface LinkPart {
  readonly id: string;
  readonly text: string;
  readonly title: MessageKey;
  readonly meta: MessageKey;
  readonly body: MessageKey;
  readonly tone: string;
  readonly bar: string;
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
  },
  {
    id: 'meta',
    text: '#3.0.',
    title: 'how.link.meta.title',
    meta: 'how.link.meta.meta',
    body: 'how.link.meta.body',
    tone: 'text-solar-300',
    bar: 'bg-solar-300/35',
  },
  {
    id: 'id',
    text: 'Q3tN0xvR8kLp2WcY5aHf1g',
    title: 'how.link.id.title',
    meta: 'how.link.id.meta',
    body: 'how.link.id.body',
    tone: 'text-rose-300',
    bar: 'bg-rose-400/35',
  },
  {
    id: 'key',
    text: '.Vh2sXq9LmB4tRk7PzW1eNc6YuJ3oFa8DiG5bTn0KwEs.T7cQm…',
    title: 'how.link.key.title',
    meta: 'how.link.key.meta',
    body: 'how.link.key.body',
    tone: 'text-aegis-300',
    bar: 'bg-aegis-300/35',
  },
];
