import type { MessageKey } from '../../../../shared/language/czech.ts';
import type { IconName } from '../../../../shared/ui/icon/icons.ts';

export type DiagramLayout = 'mobile' | 'desktop';

interface DiagramNode {
  readonly icon: IconName;
  readonly title: MessageKey;
  readonly text: MessageKey;
  readonly animation: string;
  readonly delay: string;
  readonly captionDelay: string;
  readonly badge?: { readonly icon: IconName; readonly animation: string };
}

export const DIAGRAM_NODES: readonly DiagramNode[] = [
  {
    icon: 'laptop',
    title: 'how.node.sender',
    text: 'how.node.sender.text',
    animation: 'motion-safe:animate-how-node-sender',
    delay: '[--reveal-delay:0ms]',
    captionDelay: '[--reveal-delay:160ms]',
  },
  {
    icon: 'server',
    title: 'how.node.server',
    text: 'how.node.server.text',
    animation: 'motion-safe:animate-how-node-server',
    delay: '[--reveal-delay:420ms]',
    captionDelay: '[--reveal-delay:580ms]',
    badge: { icon: 'lock', animation: 'motion-safe:animate-how-stored' },
  },
  {
    icon: 'phone',
    title: 'how.node.recipient',
    text: 'how.node.recipient.text',
    animation: 'motion-safe:animate-how-node-recipient',
    delay: '[--reveal-delay:840ms]',
    captionDelay: '[--reveal-delay:1000ms]',
    badge: { icon: 'check', animation: 'motion-safe:animate-how-opened' },
  },
];

interface Layout {
  readonly id: string;
  readonly frame: string;
  readonly viewBox: string;
  readonly arc: string;
  readonly arcFrom: number;
  readonly arcTo: number;
  readonly gap: {
    readonly x: number;
    readonly y: number;
    readonly width: number;
    readonly height: number;
    readonly radius: number;
  };
  readonly glowWidth: number;
  readonly arcWidth: number;
  readonly ends: readonly { readonly x: number; readonly y: number }[];
  readonly endHalo: number;
  readonly endDot: number;
  readonly lines: readonly string[];
  readonly chips: readonly { readonly position: string; readonly animation: string }[];
  readonly chipIcon: string;
  readonly chipStroke: number;
  readonly keyChip: string;
  readonly nodes: readonly string[];
  readonly captions: readonly string[];
  readonly captionWidth: string;
  readonly captionTitle: string;
  readonly captionText: string;
}

export const LAYOUTS: Readonly<Record<DiagramLayout, Layout>> = {
  mobile: {
    id: 'M',
    frame: 'mx-auto aspect-[360/270] max-w-[420px] md:hidden',
    viewBox: '0 0 360 270',
    arc: 'M56 110C120 10 240 10 304 110',
    arcFrom: 56,
    arcTo: 304,
    gap: { x: 109, y: 18, width: 142, height: 34, radius: 17 },
    glowWidth: 6,
    arcWidth: 1.75,
    ends: [
      { x: 304, y: 110 },
      { x: 56, y: 110 },
    ],
    endHalo: 6,
    endDot: 2.5,
    lines: ['M86 140H150', 'M210 140H274'],
    chips: [
      { position: 'left-[32.78%] top-[51.85%]', animation: 'motion-safe:animate-how-chip-a' },
      { position: 'left-[67.22%] top-[51.85%]', animation: 'motion-safe:animate-how-chip-b' },
    ],
    chipIcon: 'hidden size-3',
    chipStroke: 2.2,
    keyChip: 'left-[50.00%] top-[12.96%]',
    nodes: [
      'left-[15.56%] top-[51.85%] w-[16.67%]',
      'left-[50.00%] top-[51.85%] w-[16.67%]',
      'left-[84.44%] top-[51.85%] w-[16.67%]',
    ],
    captions: ['left-[15.56%] top-[68.15%]', 'left-[50.00%] top-[68.15%]', 'left-[84.44%] top-[68.15%]'],
    captionWidth: 'w-[31%]',
    captionTitle: 'text-ui',
    captionText: 'text-micro',
  },
  desktop: {
    id: 'D',
    frame: 'hidden aspect-[1000/320] md:block',
    viewBox: '0 0 1000 320',
    arc: 'M150 126C300 0 700 0 850 126',
    arcFrom: 150,
    arcTo: 850,
    gap: { x: 428, y: 14, width: 144, height: 35, radius: 17.5 },
    glowWidth: 7,
    arcWidth: 2,
    ends: [
      { x: 850, y: 126 },
      { x: 150, y: 126 },
    ],
    endHalo: 7.199999999999999,
    endDot: 3,
    lines: ['M194 170H456', 'M544 170H806'],
    chips: [
      { position: 'left-[32.50%] top-[53.13%]', animation: 'motion-safe:animate-how-chip-a' },
      { position: 'left-[67.50%] top-[53.13%]', animation: 'motion-safe:animate-how-chip-b' },
    ],
    chipIcon: 'size-3.5',
    chipStroke: 2,
    keyChip: 'left-[50.00%] top-[9.84%]',
    nodes: [
      'left-[15.00%] top-[53.13%] w-[8.80%]',
      'left-[50.00%] top-[53.13%] w-[8.80%]',
      'left-[85.00%] top-[53.13%] w-[8.80%]',
    ],
    captions: ['left-[15.00%] top-[73.75%]', 'left-[50.00%] top-[73.75%]', 'left-[85.00%] top-[73.75%]'],
    captionWidth: 'w-[24%]',
    captionTitle: 'text-title',
    captionText: 'text-body',
  },
};
