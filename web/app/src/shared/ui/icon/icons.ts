type Shape =
  | { readonly path: string }
  | { readonly circle: readonly [cx: number, cy: number, r: number] }
  | { readonly rect: readonly [x: number, y: number, width: number, height: number, radius: number] }
  | { readonly ellipse: readonly [cx: number, cy: number, rx: number, ry: number]; readonly rotate?: number };

const lockBody: Shape = { rect: [4.5, 10.5, 15, 10, 2.5] };
const clockFace: Shape = { circle: [12, 12, 8.5] };
const eyeLid: Shape = { path: 'M2.5 12C4 8 7.5 5 12 5s8 3 9.5 7c-1.5 4-5 7-9.5 7s-8-3-9.5-7z' };
const trashLid: Shape = { path: 'M4.5 7h15M9.5 7V5h5v2M6.5 7l1 12.5h9l1-12.5' };

export const ICONS = {
  alert: [clockFace, { path: 'M12 7.5v5.5M12 16h.01' }],
  arrowRight: [{ path: 'M5 12h14M13 6l6 6-6 6' }],
  atom: [
    { circle: [12, 12, 1.5] },
    { ellipse: [12, 12, 9, 3.6] },
    { ellipse: [12, 12, 9, 3.6], rotate: 60 },
    { ellipse: [12, 12, 9, 3.6], rotate: 120 },
  ],
  ban: [{ circle: [12, 12, 8.5] }, { path: 'M6 6l12 12' }],
  broken: [
    { path: 'M9.5 14.5l-1 1a3.5 3.5 0 0 1-5-5l2-2' },
    { path: 'M14.5 9.5l1-1a3.5 3.5 0 0 1 5 5l-2 2' },
    { path: 'M8 3.5v2M3.5 8h2M16 20.5v-2M20.5 16h-2' },
  ],
  burst: [{ path: 'M12 4v3M12 17v3M4 12h3M17 12h3M6.5 6.5l2 2M15.5 15.5l2 2M6.5 17.5l2-2M15.5 8.5l2-2' }],
  camera: [
    { path: 'M4 8h3l1.5-2h7L17 8h3a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z' },
    { circle: [12, 13, 3.5] },
  ],
  cancel: [clockFace, { path: 'M9 9l6 6M15 9l-6 6' }],
  caretDown: [{ path: 'M7 10l5 5 5-5' }],
  caution: [{ path: 'M12 4.5 3 19.5h18z' }, { path: 'M12 10v4M12 16.8h.01' }],
  check: [{ path: 'M5 12.5l4.5 4.5L19 7.5' }],
  chevronsUpDown: [{ path: 'M8 9.5l4-4 4 4M8 14.5l4 4 4-4' }],
  chevronDown: [{ path: 'M6 9l6 6 6-6' }],
  clock: [clockFace, { path: 'M12 7.5V12l3 2' }],
  close: [{ path: 'M6 6l12 12M18 6L6 18' }],
  code: [{ path: 'M8.5 7L3.5 12l5 5M15.5 7l5 5-5 5' }],
  copy: [{ rect: [8, 8, 12, 12, 2.5] }, { path: 'M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2' }],
  cross: [{ path: 'M7 7l10 10M17 7L7 17' }],
  datacenter: [
    { rect: [4, 3, 16, 18, 1.5] },
    { path: 'M4 9h16M4 15h16M7.5 6h.01M7.5 12h.01M7.5 18h.01M11.5 6h5M11.5 12h5M11.5 18h5' },
  ],
  database: [
    { ellipse: [12, 6, 7, 3] },
    { path: 'M5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6' },
    { path: 'M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6' },
  ],
  document: [
    { path: 'M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z' },
    { path: 'M14 3v5h5M9 13h6M9 17h4' },
  ],
  download: [
    { path: 'M12 4.5V15M7.5 10.5L12 15l4.5-4.5' },
    { path: 'M5 14.5v3.5a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-3.5' },
  ],
  external: [{ path: 'M7 17L17 7M9 7h8v8' }],
  envelope: [{ rect: [3, 5.5, 18, 13, 2.5] }, { path: 'M3.5 7.5l8.5 6 8.5-6' }],
  envelopeOpen: [
    { path: 'M3.5 10.5L12 4l8.5 6.5V19a1.5 1.5 0 0 1-1.5 1.5H5A1.5 1.5 0 0 1 3.5 19z' },
    { path: 'M3.5 10.5l8.5 6 8.5-6' },
  ],
  eye: [eyeLid, { circle: [12, 12, 3] }],
  eyeOff: [
    { path: 'M3 3l18 18' },
    {
      path: 'M10.6 5.1A9.9 9.9 0 0 1 12 5c5 0 8.5 4.5 9.5 7a13 13 0 0 1-2.6 3.6M6.3 6.3C4.4 7.6 3.1 9.6 2.5 12c1 2.5 4.5 7 9.5 7 1.7 0 3.2-.5 4.5-1.2',
    },
    { path: 'M9.9 9.9a3 3 0 0 0 4.2 4.2' },
  ],
  farm: [
    { rect: [2.5, 3.5, 19, 7.5, 1.5] },
    { circle: [8, 7.25, 1.75] },
    { circle: [14.5, 7.25, 1.75] },
    { rect: [2.5, 13, 19, 7.5, 1.5] },
    { circle: [8, 16.75, 1.75] },
    { circle: [14.5, 16.75, 1.75] },
  ],
  file: [{ path: 'M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z' }, { path: 'M14 3v5h5' }],
  flag: [{ path: 'M5.5 21V4.5M5.5 4.5h11l-2 4 2 4h-11' }],
  fork: [
    { circle: [12, 5, 2.2] },
    { circle: [6, 19, 2.2] },
    { circle: [18, 19, 2.2] },
    { path: 'M12 7.2v3.3M12 10.5c-3 0-6 1.6-6 4.5v1.8M12 10.5c3 0 6 1.6 6 4.5v1.8' },
  ],
  gauge: [{ path: 'M4.5 16.5a8 8 0 1 1 15 0' }, { path: 'M12 13.5l3.5-3.5' }, { circle: [12, 13.5, 1.2] }],
  globe: [
    clockFace,
    { path: 'M3.5 12h17M12 3.5c2.4 2.3 3.5 5.2 3.5 8.5s-1.1 6.2-3.5 8.5c-2.4-2.3-3.5-5.2-3.5-8.5s1.1-6.2 3.5-8.5z' },
  ],
  gpu: [
    { rect: [2.5, 4, 13, 10, 1.5] },
    { path: 'M6.5 18.5h5M9 14v4.5' },
    { rect: [18, 4, 3.5, 14.5, 1] },
    { path: 'M19.75 7.5h.01' },
  ],
  heart: [{ path: 'M12 20s-7.5-4.4-7.5-10A4.3 4.3 0 0 1 12 7.2 4.3 4.3 0 0 1 19.5 10c0 5.6-7.5 10-7.5 10z' }],
  hash: [{ path: 'M5 9h14M5 15h14M10 4 8 20M16 4l-2 16' }],
  inbox: [
    { path: 'M3.5 13.5 6 5.5h12l2.5 8v5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z' },
    { path: 'M3.5 13.5h5l1.5 2.5h4l1.5-2.5h5' },
  ],
  info: [clockFace, { path: 'M12 11v5M12 8h.01' }],
  keyhole: [{ rect: [5, 11, 14, 9, 2] }, { path: 'M8 11V8a4 4 0 0 1 8 0v3M12 15v1.5' }],
  key: [{ circle: [8, 15, 4] }, { path: 'M11 12l8-8M16 7l2 2M14 9l2 2' }],
  laptop: [{ rect: [4.5, 5, 15, 10.5, 1.5] }, { path: 'M2.5 19h19' }],
  message: [{ path: 'M20 14.5a2 2 0 0 1-2 2H8.5L4 20V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2z' }],
  link: [
    { path: 'M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1' },
    { path: 'M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1' },
  ],
  list: [{ path: 'M9 6.5h11M9 12h11M9 17.5h11' }, { path: 'M4.5 6.5h.01M4.5 12h.01M4.5 17.5h.01' }],
  lock: [lockBody, { path: 'M8 10.5V7.5a4 4 0 0 1 8 0v3' }],
  minus: [{ path: 'M6 12h12' }],
  paperclip: [
    {
      path: 'M20 11.5l-7.8 7.8a5 5 0 0 1-7.1-7.1l8.5-8.5a3.3 3.3 0 0 1 4.7 4.7l-8.4 8.4a1.7 1.7 0 0 1-2.4-2.4l7.7-7.7',
    },
  ],
  pen: [{ path: 'M4 20h4L19.5 8.5a2.1 2.1 0 0 0-3-3L5 17v3z' }, { path: 'M14.5 7.5l3 3' }],
  rack: [{ rect: [4, 4.5, 16, 6, 2] }, { rect: [4, 13.5, 16, 6, 2] }, { path: 'M8 7.5h.01M8 16.5h.01' }],
  refresh: [{ path: 'M4 12a8 8 0 1 0 2.5-5.8' }, { path: 'M4 4v4h4' }],
  person: [{ circle: [12, 8, 3.5] }, { path: 'M5 19.5c1.2-3.4 3.8-5 7-5s5.8 1.6 7 5' }],
  phone: [{ rect: [7, 3, 10, 18, 2.5] }, { path: 'M11 17.5h2' }],
  plus: [{ path: 'M12 5v14M5 12h14' }],
  search: [{ circle: [11, 11, 6] }, { path: 'M20 20l-4.5-4.5' }],
  server: [{ rect: [4, 4, 16, 7, 2] }, { rect: [4, 13, 16, 7, 2] }, { path: 'M8 7.5h.01M8 16.5h.01' }],
  share: [
    { path: 'M10 14a4.5 4.5 0 0 0 6.4 0l3-3a4.5 4.5 0 0 0-6.4-6.4l-1 1' },
    { path: 'M14 10a4.5 4.5 0 0 0-6.4 0l-3 3a4.5 4.5 0 0 0 6.4 6.4l1-1' },
  ],
  shield: [{ path: 'M12 3l8 3.5v5c0 4.5-3.4 8.4-8 9.5-4.6-1.1-8-5-8-9.5v-5z' }, { path: 'M12 8v5M12 16h.01' }],
  shieldCheck: [{ path: 'M12 3l7 3v6c0 4.5-3 7.5-7 9-4-1.5-7-4.5-7-9V6z' }, { path: 'M9 12l2 2 4-4' }],
  storage: [
    { ellipse: [12, 6, 7, 2.5] },
    { path: 'M5 6v12c0 1.4 3.1 2.5 7 2.5s7-1.1 7-2.5V6' },
    { path: 'M5 12c0 1.4 3.1 2.5 7 2.5s7-1.1 7-2.5' },
  ],
  terminal: [{ rect: [3.5, 4.5, 17, 15, 2.5] }, { path: 'M7.5 9.5l3 2.5-3 2.5M12.5 15h4' }],
  timer: [{ circle: [12, 13.5, 7.5] }, { path: 'M10 3h4M12 13.5l3-3' }],
  trash: [trashLid, { path: 'M10 11v5M14 11v5' }],
  unlock: [lockBody, { path: 'M8 10.5V7.5a4 4 0 0 1 7.6-1.7' }],
  update: [clockFace, { path: 'M12 16V8M8.5 11.5L12 8l3.5 3.5' }],
  upload: [{ path: 'M12 15V4.5M7.5 9L12 4.5 16.5 9' }, { path: 'M5 14.5v3.5a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-3.5' }],
  userOff: [{ circle: [12, 8, 3.5] }, { path: 'M5 20c.8-3.5 3.6-5.5 7-5.5s6.2 2 7 5.5' }, { path: 'M3 3l18 18' }],
  write: [{ path: 'M4 20h4L19 9l-4-4L4 16v4z' }, { path: 'M14 6l4 4' }],
  warning: [
    { path: 'M10.3 3.9L2.4 17.6a2 2 0 0 0 1.7 3h15.8a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z' },
    { path: 'M12 9.5v4M12 17h.01' },
  ],
  world: [
    { circle: [12, 12, 9] },
    { path: 'M3 12h18M12 3c2.5 2.6 3.8 5.6 3.8 9s-1.3 6.4-3.8 9M12 3C9.5 5.6 8.2 8.6 8.2 12s1.3 6.4 3.8 9' },
  ],
} satisfies Record<string, readonly Shape[]>;

export type IconName = keyof typeof ICONS;
export type { Shape };

interface IconMotion {
  readonly shape?: number;
  readonly classes: string;
}

const fillBox = '[transform-box:fill-box] origin-center';

export const ICON_MOTION: Partial<Record<IconName, IconMotion>> = {
  arrowRight: {
    classes: 'transition-transform motion-safe:action:translate-x-[3px]',
  },
  clock: {
    shape: 1,
    classes:
      'transition-transform duration-500 [transform-box:view-box] origin-[12px_12px] motion-safe:action:rotate-[120deg]',
  },
  close: {
    classes: 'transition-transform motion-safe:action:rotate-90',
  },
  copy: {
    shape: 0,
    classes: `transition-transform ${fillBox} motion-safe:action:translate-x-[1.5px] motion-safe:action:translate-y-[1.5px]`,
  },
  download: {
    classes: 'motion-safe:action:animate-icon-hop',
  },
  eye: {
    classes: 'motion-safe:action:animate-icon-blink',
  },
  external: {
    classes: 'transition-transform motion-safe:action:translate-x-[2px] motion-safe:action:-translate-y-[2px]',
  },
  eyeOff: {
    classes: 'motion-safe:action:animate-icon-blink',
  },
  flag: {
    classes: 'transition-transform origin-bottom-left motion-safe:action:-rotate-6',
  },
  globe: {
    classes: 'transition-transform duration-500 motion-safe:action:rotate-[20deg]',
  },
  key: {
    classes: 'transition-transform motion-safe:action:rotate-[20deg]',
  },
  lock: {
    shape: 1,
    classes: `transition-transform ${fillBox} motion-safe:action:-translate-y-[1.5px]`,
  },
  minus: {
    classes: 'transition-transform motion-safe:action:scale-x-75',
  },
  paperclip: {
    classes: 'transition-transform motion-safe:action:-rotate-12',
  },
  pen: {
    classes: 'transition-transform origin-bottom-left motion-safe:action:-rotate-12',
  },
  plus: {
    classes: 'transition-transform motion-safe:action:rotate-90',
  },
  refresh: {
    classes: 'transition-transform duration-500 motion-safe:action:rotate-180',
  },
  shieldCheck: {
    classes: 'transition-transform motion-safe:action:scale-110',
  },
  trash: {
    classes: 'transition-transform origin-bottom motion-safe:action:-rotate-6',
  },
};
