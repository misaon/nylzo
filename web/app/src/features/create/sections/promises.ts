import type { MessageKey } from '../../../shared/language/catalog.ts';
import { REPOSITORY_URL } from '../../../shared/shell/repository.ts';
import type { IconName } from '../../../shared/ui/icon/icons.ts';

type PromiseLink =
  | { readonly kind: 'proof'; readonly href: string; readonly target: MessageKey }
  | { readonly kind: 'page'; readonly href: string; readonly label: MessageKey };

interface PromiseCard {
  readonly key: 'promise.account' | 'promise.preview' | 'promise.logs' | 'promise.ttl' | 'promise.cancel';
  readonly icon: IconName;
  readonly link: PromiseLink;
  readonly wide: boolean;
  readonly motion: string;
}

export const PROMISES: readonly PromiseCard[] = [
  {
    key: 'promise.account',
    icon: 'userOff',
    link: { kind: 'proof', href: '/transparency#transparency-stores', target: 'promise.account.verify' },
    wide: true,
    motion: 'md:[--reveal-index:0] md:[--reveal-x:28px] md:[--reveal-y:28px]',
  },
  {
    key: 'promise.preview',
    icon: 'eye',
    link: { kind: 'proof', href: `${REPOSITORY_URL}/wiki/Protocol#4-link`, target: 'promise.preview.verify' },
    wide: false,
    motion: 'md:[--reveal-index:1] md:[--reveal-x:-28px] md:[--reveal-y:28px]',
  },
  {
    key: 'promise.logs',
    icon: 'server',
    link: { kind: 'proof', href: '/transparency#transparency-logs', target: 'promise.logs.verify' },
    wide: false,
    motion: 'md:[--reveal-index:1] md:[--reveal-x:28px] md:[--reveal-y:0px]',
  },
  {
    key: 'promise.ttl',
    icon: 'clock',
    link: { kind: 'proof', href: '/transparency#transparency-limits', target: 'promise.ttl.verify' },
    wide: false,
    motion: 'md:[--reveal-index:0] md:[--reveal-x:0px] md:[--reveal-y:0px]',
  },
  {
    key: 'promise.cancel',
    icon: 'refresh',
    link: { kind: 'page', href: '/sent', label: 'promise.cancel.link' },
    wide: false,
    motion: 'md:[--reveal-index:1] md:[--reveal-x:-28px] md:[--reveal-y:0px]',
  },
];
