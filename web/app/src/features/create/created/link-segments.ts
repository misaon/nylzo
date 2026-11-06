export interface LinkSegment {
  readonly id: 'host' | 'id' | 'meta' | 'key';
  readonly text: string;
  readonly short: string;
  readonly secret: boolean;
}

function middle(text: string, head: number, tail: number): string {
  return text.length > head + tail + 1 ? `${text.slice(0, head)}…${text.slice(-tail)}` : text;
}

export function linkSegments(link: string): LinkSegment[] {
  const hash = link.indexOf('#');
  const fields = hash === -1 ? [] : link.slice(hash + 1).split('.');
  const [version = '', kdfId = '', id = '', ...keys] = fields;
  if (!link.slice(0, hash).endsWith('/r') || fields.length !== 5) {
    return [{ id: 'host', text: link, short: link, secret: false }];
  }
  const host = link.slice(0, hash).replace(/^https?:\/\//u, '');
  const meta = `#${version}.${kdfId}.`;
  const key = `.${keys.join('.')}`;
  return [
    { id: 'host', text: host, short: host, secret: false },
    { id: 'meta', text: meta, short: meta, secret: false },
    { id: 'id', text: middle(id, 4, 3), short: middle(id, 2, 2), secret: false },
    { id: 'key', text: middle(key, 14, 6), short: middle(key, 5, 3), secret: true },
  ];
}
