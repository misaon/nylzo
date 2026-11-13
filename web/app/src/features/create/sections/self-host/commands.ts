import { REPOSITORY_URL } from '../../../../shared/shell/repository.ts';

export type InstallTab = 'cli' | 'compose' | 'binary' | 'verify';
type TokenKind = 'command' | 'word' | 'flag' | 'number' | 'operator' | 'variable' | 'argument';

export interface Token {
  readonly text: string;
  readonly kind: TokenKind;
  readonly joined?: boolean;
}

export const TOKEN_CLASSES: Readonly<Record<TokenKind, string>> = {
  command: 'font-medium text-aegis-200',
  word: 'text-ink',
  flag: 'text-nebula-300',
  number: 'text-solar-300',
  operator: 'text-ink-mute',
  variable: 'text-rose-300',
  argument: 'text-ink-soft',
};

const command = (text: string): Token => ({ text, kind: 'command' });
const word = (text: string): Token => ({ text, kind: 'word' });
const flag = (text: string): Token => ({ text, kind: 'flag' });
const number = (text: string): Token => ({ text, kind: 'number' });
const operator = (text: string, joined = false): Token => ({ text, kind: 'operator', joined });
const argument = (text: string, joined = false): Token => ({ text, kind: 'argument', joined });

const HOMEBREW_INSTALL: readonly Token[] = [command('brew'), word('install'), argument('misaon/nylzo/nylzo')];

export function installLines(tab: InstallTab, domain: string): readonly (readonly Token[])[] {
  switch (tab) {
    case 'cli': {
      return [
        HOMEBREW_INSTALL,
        [
          command('nylzo'),
          word('create'),
          flag('--server'),
          argument('https://nylzo.com'),
          flag('--ttl'),
          argument('1h'),
          flag('--opens'),
          number('1'),
          operator('<'),
          argument('message.txt'),
          operator('>'),
          argument('link.txt'),
        ],
        [
          command('nylzo'),
          word('read'),
          flag('--link-file'),
          argument('link.txt'),
          flag('--out'),
          argument('received'),
        ],
      ];
    }
    case 'compose': {
      return [
        [command('git'), word('clone'), argument(REPOSITORY_URL)],
        [command('cd'), argument('nylzo/deploy/compose')],
        [command('cp'), argument('.env.example'), argument('.env')],
        [command('nano'), argument('.env')],
        [
          command('mkdir'),
          flag('-p'),
          argument('secrets'),
          operator('&&'),
          command('openssl'),
          word('rand'),
          flag('-hex'),
          number('32'),
          operator('>'),
          argument('secrets/pepper'),
        ],
        [
          command('sudo'),
          command('chown'),
          number('65532:65532'),
          argument('secrets/pepper'),
          operator('&&'),
          command('sudo'),
          command('chmod'),
          number('0400'),
          argument('secrets/pepper'),
        ],
        [
          command('sudo'),
          command('install'),
          flag('-d'),
          flag('-o'),
          number('65533'),
          flag('-g'),
          number('65533'),
          flag('-m'),
          number('0700'),
          argument('caddy-state'),
        ],
        [
          command('sudo'),
          command('swapoff'),
          flag('-a'),
          operator('&&'),
          command('sudo'),
          command('sed'),
          flag('-i.bak'),
          argument(String.raw`'/\sswap\s/s/^/#/'`),
          argument('/etc/fstab'),
        ],
        [
          command('sudo'),
          command('cp'),
          argument('nylzo-epoch.conf'),
          argument('/etc/tmpfiles.d/'),
          operator('&&'),
          command('sudo'),
          command('systemd-tmpfiles'),
          flag('--create'),
          argument('nylzo-epoch.conf'),
        ],
        [command('docker'), word('compose'), word('up'), flag('-d')],
      ];
    }
    case 'binary': {
      return [
        HOMEBREW_INSTALL,
        [
          command('openssl'),
          word('rand'),
          flag('-hex'),
          number('32'),
          operator('>'),
          argument('pepper'),
          operator('&&'),
          command('chmod'),
          number('0400'),
          argument('pepper'),
        ],
        [
          command('env'),
          operator('$('),
          { text: 'cat', kind: 'command', joined: true },
          argument('report-key.env'),
          operator(')', true),
          { text: 'NYLZO_PEPPER_FILE', kind: 'variable' },
          operator('=', true),
          argument('pepper', true),
          command('nylzo'),
          word('serve'),
        ],
      ];
    }
    case 'verify': {
      return [
        HOMEBREW_INSTALL,
        [command('nylzo'), word('verify-client'), flag('--server'), argument(`https://${domain}`)],
      ];
    }
  }
}

export function installText(tab: InstallTab, domain: string): string {
  return installLines(tab, domain)
    .map((tokens) =>
      tokens.map((token, index) => (index === 0 || token.joined ? token.text : ` ${token.text}`)).join(''),
    )
    .join('\n');
}

export const LINE_DELAYS = [
  '[--reveal-delay:620ms]',
  '[--reveal-delay:790ms]',
  '[--reveal-delay:960ms]',
  '[--reveal-delay:1130ms]',
  '[--reveal-delay:1300ms]',
  '[--reveal-delay:1470ms]',
  '[--reveal-delay:1640ms]',
  '[--reveal-delay:1810ms]',
  '[--reveal-delay:1980ms]',
  '[--reveal-delay:2150ms]',
  '[--reveal-delay:2320ms]',
] as const;
