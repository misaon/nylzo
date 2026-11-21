import type { MessageKey } from '../../../../shared/language/catalog.ts';

export type MarkKind = 'yes' | 'strong' | 'no' | 'partial';

type CompareCell =
  | { readonly kind: MarkKind; readonly note?: MessageKey }
  | { readonly kind: 'text'; readonly note: MessageKey; readonly tone: string };

export interface CompareRow {
  readonly label: MessageKey;
  readonly cells: readonly CompareCell[];
}

export const COMPARE_ROWS: readonly CompareRow[] = [
  {
    label: 'compare.row.client',
    cells: [{ kind: 'yes' }, { kind: 'yes' }, { kind: 'yes' }, { kind: 'no' }, { kind: 'yes' }],
  },
  {
    label: 'compare.row.preview',
    cells: [
      { kind: 'yes' },
      { kind: 'yes' },
      { kind: 'yes' },
      { kind: 'yes' },
      { kind: 'partial', note: 'compare.note.nojs' },
    ],
  },
  {
    label: 'compare.row.passphrase',
    cells: [
      { kind: 'yes' },
      { kind: 'yes' },
      { kind: 'yes' },
      { kind: 'partial', note: 'compare.note.server_side' },
      { kind: 'no' },
    ],
  },
  {
    label: 'compare.row.secret',
    cells: [
      { kind: 'text', note: 'compare.note.bits256', tone: 'text-aegis-200' },
      { kind: 'text', note: 'compare.note.bits256', tone: 'text-ink-soft' },
      { kind: 'text', note: 'compare.note.bits131', tone: 'text-ink-soft' },
      { kind: 'text', note: 'compare.note.none', tone: 'text-ink-mute' },
      { kind: 'text', note: 'compare.note.bits128', tone: 'text-ink-soft' },
    ],
  },
  {
    label: 'compare.row.verify',
    cells: [
      { kind: 'strong', note: 'compare.note.every30' },
      { kind: 'no' },
      { kind: 'no' },
      { kind: 'no' },
      { kind: 'no' },
    ],
  },
  {
    label: 'compare.row.length',
    cells: [{ kind: 'strong' }, { kind: 'no' }, { kind: 'no' }, { kind: 'no' }, { kind: 'no' }],
  },
  {
    label: 'compare.row.id',
    cells: [{ kind: 'strong' }, { kind: 'no' }, { kind: 'no' }, { kind: 'no' }, { kind: 'no' }],
  },
  {
    label: 'compare.row.stolen',
    cells: [{ kind: 'strong' }, { kind: 'no' }, { kind: 'no' }, { kind: 'no' }, { kind: 'no' }],
  },
  {
    label: 'compare.row.ip',
    cells: [
      { kind: 'strong' },
      { kind: 'partial', note: 'compare.note.hmac' },
      { kind: 'no' },
      { kind: 'partial', note: 'compare.note.truncated' },
      { kind: 'no' },
    ],
  },
];
