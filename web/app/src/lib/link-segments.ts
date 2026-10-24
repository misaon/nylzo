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
  const path = link.indexOf('/m/');
  if (hash < 0 || path < 0 || path > hash) {
    return [{ id: 'host', text: link, short: link, server: true, tone: 'text-ink-soft', bar: 'bg-ink-mute' }];
  }
  const host = link.slice(0, path + 3);
  const id = link.slice(path + 3, hash);
  const fragment = link.slice(hash);
  const keyStart = fragment.indexOf('.', fragment.indexOf('.') + 1) + 1;
  const meta = keyStart > 0 ? fragment.slice(0, keyStart) : fragment;
  const key = keyStart > 0 ? fragment.slice(keyStart) : '';
  return [
    { id: 'host', text: host, short: host.replace(/^https?:\/\//, ''), server: true, tone: 'text-ink-soft', bar: 'bg-ink-mute' },
    { id: 'id', text: id, short: id.length > 6 ? `${id.slice(0, 6)}…` : id, server: true, tone: 'text-nebula-300', bar: 'bg-nebula-300' },
    { id: 'meta', text: meta, short: meta, server: false, tone: 'text-rose-300', bar: 'bg-rose-300' },
    { id: 'key', text: middle(key, 14, 6), short: middle(key, 8, 5), server: false, tone: 'text-aegis-300', bar: 'bg-aegis-400' },
  ];
}
