import { createContext } from 'svelte';

const cs = {
  'app.name': 'Nylzo',
  'action.retry': 'Zkusit znovu',
  'app.crashed': 'Stránka narazila na chybu. Načtěte ji znovu.',

  'nav.label': 'Hlavní navigace',
  'nav.create': 'Nová zpráva',
  'nav.sent': 'Odeslané',
  'nav.report': 'Nahlásit zneužití',
  'language.other': 'English',

  'create.title': 'Nová zpráva',
  'create.text': 'Text zprávy',
  'create.file': 'Příloha, nejvýš {size}',
  'create.file.clear': 'Odebrat přílohu',
  'create.ttl': 'Platnost',
  'create.opens': 'Počet otevření, nejvýš {max}',
  'create.passphrase': 'Passphrase, volitelná',
  'create.passphrase.hint': 'Nejméně {min} znaků. Pošlete ji příjemci jiným kanálem než odkaz.',
  'create.passphrase.weak': 'Doporučujeme aspoň {words} náhodná slova a zároveň {chars} znaků.',
  'create.kdf': 'Odvození klíče z passphrase',
  'create.kdf.1': 'scrypt, odolnější proti hádání passphrase',
  'create.kdf.2': 'PBKDF2, nativní funkce prohlížeče',
  'create.remember': 'Zapamatovat na tomto zařízení pro stav a zrušení',
  'create.submit': 'Vytvořit odkaz',
  'create.loading': 'Načítám nastavení služby…',
  'create.problems': 'Formulář obsahuje chyby, opravte označená pole.',

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
  'problem.passphrase.too_short': 'Passphrase musí mít aspoň {min} znaků.',
  'problem.passphrase.malformed': 'Passphrase obsahuje neplatné znaky.',
  'problem.passphrase.out_of_range': 'Zvolte způsob odvození klíče.',

  'failure.instance': 'Nastavení služby se nepodařilo načíst.',
  'failure.rate_limited': 'Za krátkou dobu přišlo příliš mnoho zpráv. Zkuste to za {time}.',
  'failure.unavailable': 'Služba teď nové zprávy nepřijímá. Zkuste to za {time}.',
  'failure.too_large': 'Zpráva je pro tuto službu příliš velká.',
  'failure.kdf': 'Toto zařízení odvození klíče nezvládlo. Zkuste PBKDF2 nebo jiné zařízení.',
  'failure.pow': 'Ochranu proti zahlcení se nepodařilo spočítat včas. Zkuste to znovu.',
  'failure.failed': 'Zprávu se nepodařilo vytvořit.',

  'created.title': 'Odkaz je připravený',
  'created.link': 'Odkaz na zprávu',
  'created.copy': 'Zkopírovat odkaz',
  'created.copied': 'Odkaz je ve schránce',
  'created.expires': 'Platí do {time}.',
  'created.passphrase': 'Passphrase pošlete jiným kanálem než odkaz.',
  'created.another': 'Nová zpráva',
  'created.remembered': 'Toto zařízení si zprávu zapamatovalo.',
  'created.status_link': 'Stav a zrušení zprávy',
  'created.not_remembered': 'Zprávu se na tomto zařízení nepodařilo zapamatovat, takže ji odtud nepůjde zrušit.',

  'sent.title': 'Odeslané zprávy',
  'sent.note': 'Seznam drží jen tento prohlížeč a odkazy na zprávy v něm nejsou. Záznam zmizí {days} po skončení zprávy.',
  'sent.loading': 'Načítám odeslané zprávy…',
  'sent.unavailable': 'Tento prohlížeč stránce nedovolí ukládat data, takže si odeslané zprávy nepamatuje.',
  'sent.empty': 'Toto zařízení si nepamatuje žádné zprávy.',
  'sent.item': 'Zpráva vytvořená {time}',
  'sent.state.available': 'Čeká na otevření.',
  'sent.state.opened': 'Otevřená.',
  'sent.state.expired': 'Platnost vypršela.',
  'sent.state.cancelled': 'Zrušená.',
  'sent.state.deleted': 'Smazaná příjemcem.',
  'sent.label.state': 'Stav',
  'sent.label.time': 'Platnost',
  'sent.label.opens': 'Otevření',
  'sent.label.attempts': 'Chybné pokusy o passphrase',
  'sent.label.first_opened': 'První otevření',
  'sent.not_opened': 'zatím ne',
  'sent.not_kept': 'už se neuchovává',
  'sent.ending': 'právě končí',
  'sent.until': 'do {time}',
  'sent.ended_at': 'skončila {time}',
  'sent.opens': '{used} z {max}',
  'sent.loading_item': 'Načítám stav…',
  'sent.unknown': 'Služba tuto zprávu už nezná.',
  'sent.failed': 'Stav se nepodařilo načíst.',
  'sent.cancel': 'Zrušit zprávu',
  'sent.cancel.confirm': 'Po zrušení zprávu už nikdo neotevře.',
  'sent.cancel.yes': 'Ano, zrušit',
  'sent.cancel.no': 'Ponechat',
  'sent.cancelled': 'Zpráva je zrušená.',
  'sent.forget': 'Zapomenout',
  'sent.forget.confirm': 'Zapomenutou zprávu odtud už nepůjde zrušit.',
  'sent.forget.yes': 'Ano, zapomenout',
  'sent.forget_all': 'Zapomenout všechny',
  'sent.forget_all.confirm': 'Zapomenuté zprávy odtud už nepůjde zrušit.',
  'sent.forget_all.yes': 'Ano, zapomenout všechny',
  'sent.keep': 'Nezapomínat',

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
  'read.lease_ended': 'Čas na opětovné načtení zprávy na této stránce vypršel. Zobrazení teď spotřebuje další otevření.',
  'read.cleared': 'Stránku jste opustili, a tak už zprávu nezobrazuje.',
  'read.device': 'Minule se odvození klíče na tomto zařízení nedokončilo. Pokud se to opakuje, otevřete odkaz na jiném zařízení.',
  'read.rejected.wrong': 'Passphrase nesedí. Zbývající pokusy bez čekání: {count}.',
  'read.rejected.wait': 'Passphrase nesedí. Další pokus je možný za {time}.',
  'read.rejected.locked': 'Zpráva je po chybných pokusech zamčená. Požádejte odesílatele o nový odkaz.',
  'read.rejected.malformed': 'Passphrase obsahuje neplatné znaky.',
  'read.rejected.wrong_or_changed': 'Zprávu se nepodařilo dešifrovat: passphrase nesedí, nebo se data změnila. Zadejte passphrase znovu.',
  'read.delete': 'Smazat zprávu bez zobrazení',
  'read.delete.rest': 'Smazat zbývající otevření',
  'read.delete.confirm': 'Po smazání zprávu už nikdo neotevře, ani vy.',
  'read.delete.yes': 'Ano, smazat',
  'read.delete.no': 'Nemazat',
  'read.deleted_rest': 'Zbývající otevření jsou smazaná.',
  'read.delete.failed': 'Zprávu se nepodařilo smazat. Zkuste to znovu.',

  'busy.deriving': 'Odvozuji klíč z passphrase…',
  'busy.claiming': 'Otevírám zprávu…',
  'busy.downloading': 'Stahuji zašifrovanou zprávu…',
  'busy.decrypting': 'Dešifruji…',

  'revealed.title': 'Obsah zprávy',
  'revealed.note': 'Po zavření stránky už zprávu neuvidíte.',
  'revealed.file': 'Příloha',
  'revealed.download': 'Stáhnout přílohu',
  'revealed.confirm.title': 'Tento typ souboru může spustit kód. Otevřete ho, jen pokud odesílateli věříte.',
  'revealed.confirm.extension': 'Přípona souboru: .{extension}',
  'revealed.confirm.none': 'Soubor nemá příponu.',
  'revealed.confirm.download': 'Přesto stáhnout',
  'revealed.confirm.cancel': 'Nestahovat',

  'report.title': 'Nahlásit zneužití',
  'report.open': 'Nahlásit tuto zprávu',
  'report.note': 'Nahlaste zprávu, která šíří podvod, malware nebo nezákonný obsah. Každé hlášení posoudí člověk a nic se nemaže automaticky. Hlášení uchováme, jen pokud zpráva v době odeslání ještě existuje.',
  'report.privacy': 'Server dostane jen id zprávy, důvod a podpis odvozený z odkazu. Odkaz ani obsah zprávy stránku neopustí.',
  'report.link': 'Odkaz na zprávu',
  'report.link.hint': 'Vložte celý odkaz včetně části za #.',
  'report.reason': 'Důvod, volitelný',
  'report.reason.hint': 'Nejvýš {bytes} bajtů. Odkazy a dlouhé kódy se z důvodu před odesláním odstraní.',
  'report.submit': 'Odeslat hlášení',
  'report.accepted': 'Hlášení jsme přijali. Uchováme ho, jen pokud zpráva ještě existuje.',
  'report.failed': 'Hlášení se nepodařilo odeslat. Zkuste to znovu.',
  'report.problem.link_invalid': 'Odkaz je neúplný nebo poškozený.',
  'report.problem.link_foreign': 'Odkaz patří jiné službě.',
  'report.problem.reason_too_long': 'Důvod je delší než {bytes} bajtů.',
} as const;

export type MessageKey = keyof typeof cs;

const en: Record<MessageKey, string> = {
  'app.name': 'Nylzo',
  'action.retry': 'Try again',
  'app.crashed': 'The page ran into an error. Reload it.',

  'nav.label': 'Main navigation',
  'nav.create': 'New message',
  'nav.sent': 'Sent',
  'nav.report': 'Report abuse',
  'language.other': 'Čeština',

  'create.title': 'New message',
  'create.text': 'Message text',
  'create.file': 'Attachment, up to {size}',
  'create.file.clear': 'Remove attachment',
  'create.ttl': 'Expires after',
  'create.opens': 'Number of opens, up to {max}',
  'create.passphrase': 'Passphrase, optional',
  'create.passphrase.hint': 'At least {min} characters. Send it to the recipient through another channel than the link.',
  'create.passphrase.weak': 'We recommend at least {words} random words and {chars} characters.',
  'create.kdf': 'Key derivation from the passphrase',
  'create.kdf.1': 'scrypt, harder to guess the passphrase',
  'create.kdf.2': 'PBKDF2, built into the browser',
  'create.remember': 'Remember on this device for status and cancelling',
  'create.submit': 'Create link',
  'create.loading': 'Loading the service settings…',
  'create.problems': 'The form has errors, correct the marked fields.',

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
  'problem.passphrase.too_short': 'The passphrase needs at least {min} characters.',
  'problem.passphrase.malformed': 'The passphrase contains invalid characters.',
  'problem.passphrase.out_of_range': 'Choose a key derivation.',

  'failure.instance': 'The service settings could not be loaded.',
  'failure.rate_limited': 'Too many messages in a short time. Try again in {time}.',
  'failure.unavailable': 'The service does not accept new messages right now. Try again in {time}.',
  'failure.too_large': 'The message is too large for this service.',
  'failure.kdf': 'This device could not derive the key. Choose PBKDF2 or another device.',
  'failure.pow': 'The anti-flooding check did not finish in time. Try again.',
  'failure.failed': 'The message could not be created.',

  'created.title': 'The link is ready',
  'created.link': 'Link to the message',
  'created.copy': 'Copy link',
  'created.copied': 'The link is on the clipboard',
  'created.expires': 'Valid until {time}.',
  'created.passphrase': 'Send the passphrase through another channel than the link.',
  'created.another': 'New message',
  'created.remembered': 'This device remembered the message.',
  'created.status_link': 'Status and cancelling',
  'created.not_remembered': 'This device could not remember the message, so you cannot cancel it from here.',

  'sent.title': 'Sent messages',
  'sent.note': 'Only this browser keeps the list, and it holds no links to the messages. An entry disappears {days} after its message ends.',
  'sent.loading': 'Loading sent messages…',
  'sent.unavailable': 'This browser does not let the page store data, so it cannot remember sent messages.',
  'sent.empty': 'This device remembers no messages.',
  'sent.item': 'Message created {time}',
  'sent.state.available': 'Waiting to be opened.',
  'sent.state.opened': 'Opened.',
  'sent.state.expired': 'Expired.',
  'sent.state.cancelled': 'Cancelled.',
  'sent.state.deleted': 'Deleted by the recipient.',
  'sent.label.state': 'Status',
  'sent.label.time': 'Validity',
  'sent.label.opens': 'Opens',
  'sent.label.attempts': 'Wrong passphrase attempts',
  'sent.label.first_opened': 'First opened',
  'sent.not_opened': 'not yet',
  'sent.not_kept': 'no longer kept',
  'sent.ending': 'ending now',
  'sent.until': 'until {time}',
  'sent.ended_at': 'ended {time}',
  'sent.opens': '{used} of {max}',
  'sent.loading_item': 'Loading the status…',
  'sent.unknown': 'The service no longer knows this message.',
  'sent.failed': 'The status could not be loaded.',
  'sent.cancel': 'Cancel message',
  'sent.cancel.confirm': 'Once cancelled, nobody can open the message.',
  'sent.cancel.yes': 'Yes, cancel',
  'sent.cancel.no': 'Keep it',
  'sent.cancelled': 'The message is cancelled.',
  'sent.forget': 'Forget',
  'sent.forget.confirm': 'You cannot cancel a forgotten message from here.',
  'sent.forget.yes': 'Yes, forget',
  'sent.forget_all': 'Forget all',
  'sent.forget_all.confirm': 'You cannot cancel forgotten messages from here.',
  'sent.forget_all.yes': 'Yes, forget all',
  'sent.keep': 'Do not forget',

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
  'read.lease_ended': 'The time to load the message again on this page ran out. Showing it now uses another open.',
  'read.cleared': 'You left the page, so it no longer shows the message.',
  'read.device': 'The key derivation did not finish on this device last time. If it happens again, open the link on another device.',
  'read.rejected.wrong': 'The passphrase does not match. Attempts left without waiting: {count}.',
  'read.rejected.wait': 'The passphrase does not match. The next attempt is possible in {time}.',
  'read.rejected.locked': 'Wrong attempts locked the message. Ask the sender for a new link.',
  'read.rejected.malformed': 'The passphrase contains invalid characters.',
  'read.rejected.wrong_or_changed': 'The message could not be decrypted: the passphrase does not match or the data changed. Enter the passphrase again.',
  'read.delete': 'Delete without showing',
  'read.delete.rest': 'Delete the remaining opens',
  'read.delete.confirm': 'Once deleted, nobody can open the message, you included.',
  'read.delete.yes': 'Yes, delete',
  'read.delete.no': 'Do not delete',
  'read.deleted_rest': 'The remaining opens are deleted.',
  'read.delete.failed': 'The message could not be deleted. Try again.',

  'busy.deriving': 'Deriving the key from the passphrase…',
  'busy.claiming': 'Opening the message…',
  'busy.downloading': 'Downloading the encrypted message…',
  'busy.decrypting': 'Decrypting…',

  'revealed.title': 'Message content',
  'revealed.note': 'Once you close this page, the message is gone.',
  'revealed.file': 'Attachment',
  'revealed.download': 'Download attachment',
  'revealed.confirm.title': 'This type of file can run code. Open it only if you trust the sender.',
  'revealed.confirm.extension': 'File extension: .{extension}',
  'revealed.confirm.none': 'The file has no extension.',
  'revealed.confirm.download': 'Download anyway',
  'revealed.confirm.cancel': 'Do not download',

  'report.title': 'Report abuse',
  'report.open': 'Report this message',
  'report.note': 'Report a message that spreads fraud, malware or illegal content. A person reviews every report, and nothing is deleted automatically. We keep a report only if the message still exists when you send it.',
  'report.privacy': 'The server receives only the message id, the reason and a signature derived from the link. Neither the link nor the content leaves this page.',
  'report.link': 'Link to the message',
  'report.link.hint': 'Paste the whole link, including the part after #.',
  'report.reason': 'Reason, optional',
  'report.reason.hint': 'Up to {bytes} bytes. Links and long codes are removed from the reason before it is sent.',
  'report.submit': 'Send report',
  'report.accepted': 'We received the report. We keep it only if the message still exists.',
  'report.failed': 'The report could not be sent. Try again.',
  'report.problem.link_invalid': 'The link is incomplete or damaged.',
  'report.problem.link_foreign': 'The link belongs to another service.',
  'report.problem.reason_too_long': 'The reason is longer than {bytes} bytes.',
};

export const dictionaries = { cs, en } as const;

export type Language = keyof typeof dictionaries;

export type Translate = (key: MessageKey, values?: Readonly<Record<string, string | number>>) => string;

const LANGUAGE_KEY = 'nylzo:lang';

export interface PreferenceStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export class I18n {
  language = $state<Language>('en');

  constructor(language: Language) {
    this.language = language;
  }

  readonly t: Translate = (key, values = {}) =>
    dictionaries[this.language][key].replace(/\{(\w+)\}/g, (match, name: string) => (name in values ? String(values[name]) : match));
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
  return new I18n(language);
}

export function browserPreferences(): PreferenceStore | undefined {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
}

export function otherLanguage(language: Language): Language {
  return language === 'cs' ? 'en' : 'cs';
}

export function savedLanguage(store: PreferenceStore | undefined, preferred: readonly string[]): Language {
  let saved: string | null = null;
  try {
    saved = store?.getItem(LANGUAGE_KEY) ?? null;
  } catch {
    saved = null;
  }
  return saved === 'cs' || saved === 'en' ? saved : pickLanguage(preferred);
}

export function saveLanguage(store: PreferenceStore | undefined, language: Language): void {
  try {
    store?.setItem(LANGUAGE_KEY, language);
  } catch {
    return;
  }
}

export const [getI18n, setI18n] = createContext<I18n>();
