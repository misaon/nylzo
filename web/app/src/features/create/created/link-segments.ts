export interface LinkSegment {
  readonly id: 'host' | 'id' | 'meta' | 'key';
  readonly text: string;
  readonly short: string;
}

interface KeptCharacters {
  readonly head: number;
  readonly tail: number;
}

function middle(text: string, { head, tail }: KeptCharacters): string {
  return text.length > head + tail + 1 ? `${text.slice(0, head)}…${text.slice(-tail)}` : text;
}

export function linkSegments(link: string): LinkSegment[] {
  const hash = link.indexOf('#');
  const fields = hash === -1 ? [] : link.slice(hash + 1).split('.');
  const [version = '', kdfId = '', id = '', ...keys] = fields;
  if (!link.slice(0, hash).endsWith('/r') || fields.length !== 5) {
    return [{ id: 'host', text: link, short: link }];
  }
  const host = link.slice(0, hash).replace(/^https?:\/\//u, '');
  const meta = `#${version}.${kdfId}.`;
  const key = `.${keys.join('.')}`;
  return [
    { id: 'host', text: host, short: host },
    { id: 'meta', text: meta, short: meta },
    { id: 'id', text: middle(id, { head: 4, tail: 3 }), short: middle(id, { head: 2, tail: 2 }) },
    { id: 'key', text: middle(key, { head: 14, tail: 6 }), short: middle(key, { head: 5, tail: 3 }) },
  ];
}
