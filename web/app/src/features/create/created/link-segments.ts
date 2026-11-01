export interface LinkSegment {
  readonly id: 'host' | 'id' | 'meta' | 'key';
  readonly text: string;
  readonly short: string;
  readonly server: boolean;
  readonly tone: string;
  readonly bar: string;
}

function middle(text: string, head: number, tail: number): string {
  return text.length > head + tail + 1 ? `${text.slice(0, head)}…${text.slice(-tail)}` : text;
}

export function linkSegments(link: string): LinkSegment[] {
  const hash = link.indexOf('#');
  const fields = hash === -1 ? [] : link.slice(hash + 1).split('.');
  const [version = '', kdfId = '', id = '', ...keys] = fields;
  if (!link.slice(0, hash).endsWith('/r') || fields.length !== 5) {
    return [{ id: 'host', text: link, short: link, server: true, tone: 'text-ink-soft', bar: 'bg-ink-mute' }];
  }
  const host = link.slice(0, hash);
  const meta = `#${version}.${kdfId}.`;
  const key = `.${keys.join('.')}`;
  return [
    {
      id: 'host',
      text: host,
      short: host.replace(/^https?:\/\//, ''),
      server: true,
      tone: 'text-ink-soft',
      bar: 'bg-ink-mute',
    },
    { id: 'meta', text: meta, short: meta, server: false, tone: 'text-rose-300', bar: 'bg-rose-300' },
    {
      id: 'id',
      text: id,
      short: id.length > 6 ? `${id.slice(0, 6)}…` : id,
      server: true,
      tone: 'text-nebula-300',
      bar: 'bg-nebula-300',
    },
    {
      id: 'key',
      text: middle(key, 14, 6),
      short: middle(key, 8, 5),
      server: false,
      tone: 'text-aegis-300',
      bar: 'bg-aegis-400',
    },
  ];
}
