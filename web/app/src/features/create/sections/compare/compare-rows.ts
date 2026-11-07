import type { MessageKey } from '../../../../shared/language/czech.ts';

export type MarkKind = 'yes' | 'strong' | 'no' | 'partial';

interface CompareCell {
  readonly kind: MarkKind | 'text';
  readonly note?: MessageKey;
  readonly tone?: string;
  readonly delay: string;
}

export interface CompareRow {
  readonly label: MessageKey;
  readonly fade: string;
  readonly cells: readonly CompareCell[];
}

export const HEAD_FADE = '[--rv-delay:150ms]';

export const COMPARE_ROWS: readonly CompareRow[] = [
  {
    label: 'compare.row.client',
    fade: '[--rv-delay:195ms]',
    cells: [
      { kind: 'yes', delay: '[--rv-delay:555ms]' },
      { kind: 'yes', delay: '[--rv-delay:725ms]' },
      { kind: 'yes', delay: '[--rv-delay:895ms]' },
      { kind: 'no', delay: '[--rv-delay:1065ms]' },
      { kind: 'yes', delay: '[--rv-delay:1235ms]' },
    ],
  },
  {
    label: 'compare.row.preview',
    fade: '[--rv-delay:240ms]',
    cells: [
      { kind: 'yes', delay: '[--rv-delay:590ms]' },
      { kind: 'yes', delay: '[--rv-delay:760ms]' },
      { kind: 'yes', delay: '[--rv-delay:930ms]' },
      { kind: 'yes', delay: '[--rv-delay:1100ms]' },
      { kind: 'partial', note: 'compare.note.nojs', delay: '[--rv-delay:1270ms]' },
    ],
  },
  {
    label: 'compare.row.passphrase',
    fade: '[--rv-delay:285ms]',
    cells: [
      { kind: 'yes', delay: '[--rv-delay:625ms]' },
      { kind: 'yes', delay: '[--rv-delay:795ms]' },
      { kind: 'yes', delay: '[--rv-delay:965ms]' },
      { kind: 'partial', note: 'compare.note.server_side', delay: '[--rv-delay:1135ms]' },
      { kind: 'no', delay: '[--rv-delay:1305ms]' },
    ],
  },
  {
    label: 'compare.row.secret',
    fade: '[--rv-delay:330ms]',
    cells: [
      { kind: 'text', note: 'compare.note.bits256', tone: 'text-aegis-200', delay: '[--rv-delay:660ms]' },
      { kind: 'text', note: 'compare.note.bits256', tone: 'text-ink-soft', delay: '[--rv-delay:830ms]' },
      { kind: 'text', note: 'compare.note.bits131', tone: 'text-ink-soft', delay: '[--rv-delay:1000ms]' },
      { kind: 'text', note: 'compare.note.none', tone: 'text-ink-mute', delay: '[--rv-delay:1170ms]' },
      { kind: 'text', note: 'compare.note.bits128', tone: 'text-ink-soft', delay: '[--rv-delay:1340ms]' },
    ],
  },
  {
    label: 'compare.row.verify',
    fade: '[--rv-delay:375ms]',
    cells: [
      { kind: 'partial', note: 'compare.note.verify_client', delay: '[--rv-delay:695ms]' },
      { kind: 'no', delay: '[--rv-delay:865ms]' },
      { kind: 'no', delay: '[--rv-delay:1035ms]' },
      { kind: 'no', delay: '[--rv-delay:1205ms]' },
      { kind: 'no', delay: '[--rv-delay:1375ms]' },
    ],
  },
  {
    label: 'compare.row.length',
    fade: '[--rv-delay:420ms]',
    cells: [
      { kind: 'strong', delay: '[--rv-delay:730ms]' },
      { kind: 'no', delay: '[--rv-delay:900ms]' },
      { kind: 'no', delay: '[--rv-delay:1070ms]' },
      { kind: 'no', delay: '[--rv-delay:1240ms]' },
      { kind: 'no', delay: '[--rv-delay:1410ms]' },
    ],
  },
  {
    label: 'compare.row.id',
    fade: '[--rv-delay:465ms]',
    cells: [
      { kind: 'strong', delay: '[--rv-delay:765ms]' },
      { kind: 'no', delay: '[--rv-delay:935ms]' },
      { kind: 'no', delay: '[--rv-delay:1105ms]' },
      { kind: 'no', delay: '[--rv-delay:1275ms]' },
      { kind: 'no', delay: '[--rv-delay:1445ms]' },
    ],
  },
  {
    label: 'compare.row.stolen',
    fade: '[--rv-delay:510ms]',
    cells: [
      { kind: 'strong', delay: '[--rv-delay:800ms]' },
      { kind: 'no', delay: '[--rv-delay:970ms]' },
      { kind: 'no', delay: '[--rv-delay:1140ms]' },
      { kind: 'no', delay: '[--rv-delay:1310ms]' },
      { kind: 'no', delay: '[--rv-delay:1480ms]' },
    ],
  },
  {
    label: 'compare.row.ip',
    fade: '[--rv-delay:555ms]',
    cells: [
      { kind: 'strong', delay: '[--rv-delay:835ms]' },
      { kind: 'partial', note: 'compare.note.hmac', delay: '[--rv-delay:1005ms]' },
      { kind: 'no', delay: '[--rv-delay:1175ms]' },
      { kind: 'partial', note: 'compare.note.truncated', delay: '[--rv-delay:1345ms]' },
      { kind: 'no', delay: '[--rv-delay:1515ms]' },
    ],
  },
  {
    label: 'compare.row.audit',
    fade: '[--rv-delay:600ms]',
    cells: [
      { kind: 'no', delay: '[--rv-delay:870ms]' },
      { kind: 'no', delay: '[--rv-delay:1040ms]' },
      { kind: 'no', delay: '[--rv-delay:1210ms]' },
      { kind: 'no', delay: '[--rv-delay:1380ms]' },
      { kind: 'no', note: 'compare.note.send_audits', delay: '[--rv-delay:1550ms]' },
    ],
  },
];
