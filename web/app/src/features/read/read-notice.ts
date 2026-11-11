import { formatWait } from '../../shared/language/format.ts';
import type { Translator } from '../../shared/language/translator.svelte.ts';
import type { IconName } from '../../shared/ui/icon/icons.ts';
import type { BurnOutcome, ReadView } from './reader.ts';

export type Notice =
  | 'busy'
  | 'burned'
  | 'cancelled'
  | 'cleared'
  | 'deleted'
  | 'expired'
  | 'failed'
  | 'invalid'
  | 'loading'
  | 'locked'
  | 'newer'
  | 'opened'
  | 'released'
  | 'removed'
  | 'reopen'
  | 'retired'
  | 'tampered'
  | 'timed_out'
  | 'unknown';

export interface Burned {
  readonly outcome: BurnOutcome;
  readonly afterReveal: boolean;
}

export const NOTICE_ICONS: Readonly<Record<Notice, IconName>> = {
  loading: 'envelope',
  opened: 'envelopeOpen',
  expired: 'clock',
  cancelled: 'cancel',
  deleted: 'trash',
  burned: 'trash',
  locked: 'lock',
  unknown: 'search',
  invalid: 'broken',
  newer: 'update',
  released: 'trash',
  removed: 'eyeOff',
  reopen: 'link',
  retired: 'key',
  tampered: 'shield',
  failed: 'alert',
  busy: 'clock',
  cleared: 'eyeOff',
  timed_out: 'timer',
};

const ALARMING: ReadonlySet<Notice> = new Set(['failed', 'invalid', 'locked', 'tampered']);
const ENDED: ReadonlySet<Notice> = new Set([
  'burned',
  'cancelled',
  'deleted',
  'expired',
  'invalid',
  'locked',
  'newer',
  'opened',
  'released',
  'retired',
  'tampered',
  'unknown',
]);

export function isEnded(notice: Notice): boolean {
  return ENDED.has(notice);
}

export function isAlarming(notice: Notice): boolean {
  return ALARMING.has(notice);
}

export function noticeOf(view: ReadView, burned: Burned | undefined): Notice {
  switch (view.view) {
    case 'ended': {
      const burnedBeforeReveal = burned?.outcome.outcome === 'deleted' && !burned.afterReveal;
      return burnedBeforeReveal && view.state === 'deleted' ? 'burned' : view.state;
    }
    case 'ready': {
      return view.rejected?.reason === 'wrong' && view.rejected.locked ? 'locked' : 'loading';
    }
    case 'retry_later': {
      return 'busy';
    }
    case 'cleared':
    case 'failed':
    case 'invalid':
    case 'newer':
    case 'released':
    case 'removed':
    case 'reopen':
    case 'retired':
    case 'tampered':
    case 'timed_out':
    case 'unknown': {
      return view.view;
    }
    case 'busy':
    case 'loading':
    case 'revealed': {
      return 'loading';
    }
  }
}

export function burnText({ outcome, afterReveal }: Burned, { translate, plural }: Translator): string {
  switch (outcome.outcome) {
    case 'deleted': {
      return afterReveal ? translate('read.deleted_rest') : '';
    }
    case 'ended': {
      return afterReveal ? translate(`read.ended.${outcome.state}`) : '';
    }
    case 'retry_later': {
      return translate('read.retry_later', { time: formatWait(outcome.retryAfter, plural) });
    }
    case 'unknown': {
      return afterReveal ? translate('read.delete.failed') : '';
    }
    case 'failed': {
      return translate('read.delete.failed');
    }
    case 'waiting': {
      return outcome.locked
        ? translate('read.delete.locked')
        : translate('read.delete.wait', { time: formatWait(outcome.retryAfter, plural) });
    }
    case 'ignored':
    case 'rejected': {
      return '';
    }
  }
}
