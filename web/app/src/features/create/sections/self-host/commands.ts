import { REPOSITORY_URL } from '../../../../shared/shell/repository.ts';

export type InstallTab = 'compose' | 'binary' | 'verify';
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

export function installLines(tab: InstallTab, domain: string): readonly (readonly Token[])[] {
  switch (tab) {
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
        [command('docker'), word('compose'), word('up'), flag('-d')],
      ];
    }
    case 'binary': {
      return [
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
      return [[command('nylzo'), word('verify-client'), flag('--server'), argument(`https://${domain}`)]];
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
  '[--rv-delay:620ms]',
  '[--rv-delay:790ms]',
  '[--rv-delay:960ms]',
  '[--rv-delay:1130ms]',
  '[--rv-delay:1300ms]',
  '[--rv-delay:1470ms]',
  '[--rv-delay:1640ms]',
  '[--rv-delay:1810ms]',
] as const;
