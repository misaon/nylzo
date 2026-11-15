import { REPOSITORY_URL } from '../../../shared/shell/repository.ts';
import type { IconName } from '../../../shared/ui/icon/icons.ts';

interface PromiseCard {
  readonly key: 'promise.account' | 'promise.preview' | 'promise.logs' | 'promise.ttl' | 'promise.cancel';
  readonly icon: IconName;
  readonly verify: string;
  readonly wide: boolean;
  readonly motion: string;
}

export const PROMISES: readonly PromiseCard[] = [
  {
    key: 'promise.account',
    icon: 'userOff',
    verify: '/transparency#transparency-stores',
    wide: true,
    motion: 'md:[--reveal-index:0] md:[--reveal-x:28px] md:[--reveal-y:28px]',
  },
  {
    key: 'promise.preview',
    icon: 'eye',
    verify: `${REPOSITORY_URL}/wiki/Protocol#4-link`,
    wide: false,
    motion: 'md:[--reveal-index:1] md:[--reveal-x:-28px] md:[--reveal-y:28px]',
  },
  {
    key: 'promise.logs',
    icon: 'server',
    verify: '/transparency#transparency-logs',
    wide: false,
    motion: 'md:[--reveal-index:1] md:[--reveal-x:28px] md:[--reveal-y:0px]',
  },
  {
    key: 'promise.ttl',
    icon: 'clock',
    verify: '/transparency#transparency-limits',
    wide: false,
    motion: 'md:[--reveal-index:0] md:[--reveal-x:0px] md:[--reveal-y:0px]',
  },
  {
    key: 'promise.cancel',
    icon: 'refresh',
    verify: '/sent',
    wide: false,
    motion: 'md:[--reveal-index:1] md:[--reveal-x:-28px] md:[--reveal-y:0px]',
  },
];
