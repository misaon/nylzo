import { createContext } from 'svelte';

const cs = {
  'app.name': 'Nylzo',
  'action.retry': 'Zkusit znovu',

  'create.title': 'Nová zpráva',
  'create.text': 'Text zprávy',
  'create.file': 'Příloha, nejvýš {size}',
  'create.file.clear': 'Odebrat přílohu',
  'create.ttl': 'Platnost',
  'create.opens': 'Počet otevření, nejvýš {max}',
  'create.passphrase': 'Passphrase, volitelná',
  'create.passphrase.hint': 'Nejméně 12 znaků. Pošlete ji příjemci jiným kanálem než odkaz.',
  'create.passphrase.weak': 'Doporučujeme aspoň 4 náhodná slova nebo 16 znaků.',
  'create.kdf': 'Odvození klíče z passphrase',
  'create.kdf.1': 'scrypt, odolnější proti hádání passphrase',
  'create.kdf.2': 'PBKDF2, nativní funkce prohlížeče',
  'create.submit': 'Vytvořit odkaz',
  'create.loading': 'Načítám nastavení služby…',

  'ttl.3600': '1 hodina',
  'ttl.86400': '24 hodin',
  'ttl.259200': '3 dny',
  'ttl.604800': '7 dní',

  'progress.preparing': 'Připravuji zprávu…',
  'progress.deriving': 'Odvozuji klíč z passphrase…',
  'progress.proving': 'Počítám ochranu proti zahlcení služby…',
  'progress.uploading': 'Nahrávám zašifrovanou zprávu…',

  'problem.content.missing': 'Napište text nebo přiložte soubor.',
  'problem.text.too_large': 'Text je delší než {size}.',
  'problem.file.too_large': 'Soubor je větší než {size}.',
  'problem.ttl.out_of_range': 'Zvolte platnost z nabídky.',
  'problem.opens.out_of_range': 'Počet otevření musí být 1 až {max}.',
  'problem.passphrase.too_short': 'Passphrase musí mít aspoň 12 znaků.',
  'problem.passphrase.malformed': 'Passphrase obsahuje neplatné znaky.',
  'problem.passphrase.out_of_range': 'Zvolte způsob odvození klíče.',

  'failure.instance': 'Nastavení služby se nepodařilo načíst.',
  'failure.rate_limited': 'Za krátkou dobu přišlo příliš mnoho zpráv. Zkuste to za {time}.',
  'failure.unavailable': 'Služba teď nové zprávy nepřijímá. Zkuste to za {time}.',
  'failure.too_large': 'Zpráva je pro tuto službu příliš velká.',
  'failure.kdf': 'Toto zařízení odvození klíče nezvládlo. Zkuste PBKDF2 nebo jiné zařízení.',
  'failure.failed': 'Zprávu se nepodařilo vytvořit.',

  'created.title': 'Odkaz je připravený',
  'created.link': 'Odkaz na zprávu',
  'created.copy': 'Zkopírovat odkaz',
  'created.copied': 'Odkaz je ve schránce',
  'created.expires': 'Platí do {time}.',
  'created.passphrase': 'Passphrase pošlete jiným kanálem než odkaz.',
  'created.another': 'Nová zpráva',

  'read.title': 'Jednorázová zpráva',
  'read.loading': 'Načítám zprávu…',
  'read.invalid': 'Odkaz je neúplný nebo poškozený. Zkontrolujte, že jste zkopírovali celý odkaz včetně části za #.',
  'read.unknown': 'Zpráva neexistuje nebo ji už nejde otevřít.',
  'read.tampered': 'Zprávu nejde bezpečně otevřít: server nebo spojení vrátilo jiná data, než odesílatel vytvořil.',
  'read.failed': 'Něco se pokazilo. Zkuste to znovu.',
  'read.retry_later': 'Služba je přetížená. Zkuste to za {time}.',
  'read.ended.opened': 'Zpráva už byla otevřena.',
  'read.ended.expired': 'Platnost zprávy vypršela.',
  'read.ended.cancelled': 'Odesílatel zprávu zrušil.',
  'read.ended.deleted': 'Zpráva byla smazána.',
  'read.opened_at': 'Poprvé otevřena {time}.',
  'read.expires': 'Platí do {time}.',
  'read.opens': 'Zbývající otevření: {count}',
  'read.passphrase_required': 'Zpráva je chráněná passphrase.',
  'read.passphrase': 'Passphrase',
  'read.reveal': 'Zobrazit zprávu',
  'read.reveal.note': 'Zobrazení spotřebuje jedno otevření.',
  'read.resumable': 'Zprávu jste na této stránce už otevřeli a můžete ji načíst znovu, dokud běží lhůta.',
  'read.device': 'Minule se odvození klíče na tomto zařízení nedokončilo. Pokud se to opakuje, otevřete odkaz na jiném zařízení.',
  'read.rejected.wrong': 'Passphrase nesedí. Zbývající pokusy bez čekání: {count}.',
  'read.rejected.wait': 'Passphrase nesedí. Další pokus je možný za {time}.',
  'read.rejected.locked': 'Zpráva je po chybných pokusech zamčená. Požádejte odesílatele o nový odkaz.',
  'read.rejected.malformed': 'Passphrase obsahuje neplatné znaky.',

  'busy.deriving': 'Odvozuji klíč z passphrase…',
  'busy.claiming': 'Otevírám zprávu…',
  'busy.downloading': 'Stahuji zašifrovanou zprávu…',
  'busy.decrypting': 'Dešifruji…',

  'revealed.title': 'Obsah zprávy',
  'revealed.note': 'Po zavření stránky už zprávu neuvidíte.',
  'revealed.download': 'Stáhnout {name}, {size}',
  'revealed.risky': 'Tento typ souboru může spustit kód. Otevřete ho, jen pokud odesílateli věříte.',
} as const;

export type MessageKey = keyof typeof cs;

const en: Record<MessageKey, string> = {
  'app.name': 'Nylzo',
  'action.retry': 'Try again',

  'create.title': 'New message',
  'create.text': 'Message text',
  'create.file': 'Attachment, up to {size}',
  'create.file.clear': 'Remove attachment',
  'create.ttl': 'Expires after',
  'create.opens': 'Number of opens, up to {max}',
  'create.passphrase': 'Passphrase, optional',
  'create.passphrase.hint': 'At least 12 characters. Send it to the recipient through another channel than the link.',
  'create.passphrase.weak': 'We recommend at least 4 random words or 16 characters.',
  'create.kdf': 'Key derivation from the passphrase',
  'create.kdf.1': 'scrypt, harder to guess the passphrase',
  'create.kdf.2': 'PBKDF2, built into the browser',
  'create.submit': 'Create link',
  'create.loading': 'Loading the service settings…',

  'ttl.3600': '1 hour',
  'ttl.86400': '24 hours',
  'ttl.259200': '3 days',
  'ttl.604800': '7 days',

  'progress.preparing': 'Preparing the message…',
  'progress.deriving': 'Deriving the key from the passphrase…',
  'progress.proving': 'Working on the anti-flooding check…',
  'progress.uploading': 'Uploading the encrypted message…',

  'problem.content.missing': 'Write a text or attach a file.',
  'problem.text.too_large': 'The text is longer than {size}.',
  'problem.file.too_large': 'The file is larger than {size}.',
  'problem.ttl.out_of_range': 'Choose an expiry from the list.',
  'problem.opens.out_of_range': 'The number of opens must be 1 to {max}.',
  'problem.passphrase.too_short': 'The passphrase needs at least 12 characters.',
  'problem.passphrase.malformed': 'The passphrase contains invalid characters.',
  'problem.passphrase.out_of_range': 'Choose a key derivation.',

  'failure.instance': 'The service settings could not be loaded.',
  'failure.rate_limited': 'Too many messages in a short time. Try again in {time}.',
  'failure.unavailable': 'The service does not accept new messages right now. Try again in {time}.',
  'failure.too_large': 'The message is too large for this service.',
  'failure.kdf': 'This device could not derive the key. Choose PBKDF2 or another device.',
  'failure.failed': 'The message could not be created.',

  'created.title': 'The link is ready',
  'created.link': 'Link to the message',
  'created.copy': 'Copy link',
  'created.copied': 'The link is on the clipboard',
  'created.expires': 'Valid until {time}.',
  'created.passphrase': 'Send the passphrase through another channel than the link.',
  'created.another': 'New message',

  'read.title': 'One-time message',
  'read.loading': 'Loading the message…',
  'read.invalid': 'The link is incomplete or damaged. Check that you copied the whole link, including the part after #.',
  'read.unknown': 'The message does not exist or can no longer be opened.',
  'read.tampered': 'The message cannot be opened safely: the server or the connection returned other data than the sender created.',
  'read.failed': 'Something went wrong. Try again.',
  'read.retry_later': 'The service is busy. Try again in {time}.',
  'read.ended.opened': 'The message has already been opened.',
  'read.ended.expired': 'The message has expired.',
  'read.ended.cancelled': 'The sender cancelled the message.',
  'read.ended.deleted': 'The message was deleted.',
  'read.opened_at': 'First opened {time}.',
  'read.expires': 'Valid until {time}.',
  'read.opens': 'Opens left: {count}',
  'read.passphrase_required': 'The message is protected by a passphrase.',
  'read.passphrase': 'Passphrase',
  'read.reveal': 'Show message',
  'read.reveal.note': 'Showing the message uses one open.',
  'read.resumable': 'You already opened the message on this page and can load it again while the lease runs.',
  'read.device': 'The key derivation did not finish on this device last time. If it happens again, open the link on another device.',
  'read.rejected.wrong': 'The passphrase does not match. Attempts left without waiting: {count}.',
  'read.rejected.wait': 'The passphrase does not match. The next attempt is possible in {time}.',
  'read.rejected.locked': 'Wrong attempts locked the message. Ask the sender for a new link.',
  'read.rejected.malformed': 'The passphrase contains invalid characters.',

  'busy.deriving': 'Deriving the key from the passphrase…',
  'busy.claiming': 'Opening the message…',
  'busy.downloading': 'Downloading the encrypted message…',
  'busy.decrypting': 'Decrypting…',

  'revealed.title': 'Message content',
  'revealed.note': 'Once you close this page, the message is gone.',
  'revealed.download': 'Download {name}, {size}',
  'revealed.risky': 'This type of file can run code. Open it only if you trust the sender.',
};

export const dictionaries = { cs, en } as const;

export type Language = keyof typeof dictionaries;

export type Translate = (key: MessageKey, values?: Readonly<Record<string, string | number>>) => string;

export interface I18n {
  readonly language: Language;
  readonly t: Translate;
}

export function pickLanguage(preferred: readonly string[]): Language {
  for (const tag of preferred) {
    const base = tag.toLowerCase().split('-')[0];
    if (base === 'cs' || base === 'sk') {
      return 'cs';
    }
    if (base === 'en') {
      return 'en';
    }
  }
  return 'en';
}

export function createI18n(language: Language): I18n {
  const dictionary = dictionaries[language];
  const t: Translate = (key, values = {}) =>
    dictionary[key].replace(/\{(\w+)\}/g, (match, name: string) => (name in values ? String(values[name]) : match));
  return { language, t };
}

export const [getI18n, setI18n] = createContext<I18n>();
