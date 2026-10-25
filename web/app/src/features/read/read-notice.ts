import type { IconName } from '../../shared/ui/icons.ts';
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
  | 'removed'
  | 'retired'
  | 'tampered'
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
  removed: 'eyeOff',
  retired: 'key',
  tampered: 'shield',
  failed: 'alert',
  busy: 'clock',
  cleared: 'eyeOff',
};

const ALARMING: ReadonlySet<Notice> = new Set(['failed', 'invalid', 'locked', 'tampered']);

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
    case 'removed':
    case 'retired':
    case 'tampered':
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
