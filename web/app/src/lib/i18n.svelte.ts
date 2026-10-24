import { createContext } from 'svelte';

import { copyCs, copyEn } from './i18n-copy.ts';
import { homeCs, homeEn } from './i18n-home.ts';

export const baseCs = {
  'action.retry': 'Zkusit znovu',
  'app.crashed': 'Stránka narazila na chybu. Načtěte ji znovu.',

  'nav.label': 'Hlavní navigace',

  'create.title': 'Nová zpráva',
  'create.text': 'Text zprávy',
  'create.file.clear': 'Odebrat přílohu',
  'create.passphrase': 'Passphrase, volitelná',
  'create.passphrase.hint': 'Nejméně {min} znaků. Pošlete ji příjemci jiným kanálem než odkaz.',
  'create.passphrase.weak': 'Doporučujeme aspoň {words} náhodná slova a zároveň {chars} znaků.',
  'create.submit': 'Vytvořit odkaz',

  'ttl.3600': '1 hodina',
  'ttl.86400': '24 hodin',
  'ttl.259200': '3 dny',
  'ttl.604800': '7 dní',


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
  'failure.kdf': 'Toto zařízení odvození klíče z passphrase nezvládlo. Zkuste jiné zařízení.',
  'failure.pow': 'Ochranu proti zahlcení se nepodařilo spočítat včas. Zkuste to znovu.',
  'failure.failed': 'Zprávu se nepodařilo vytvořit.',

  'created.title': 'Odkaz je připravený',
  'created.link': 'Odkaz na zprávu',
  'created.copy': 'Zkopírovat odkaz',
  'created.copied': 'Odkaz je ve schránce',
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
  'sent.state.deleted': 'Smazal ji příjemce, nebo ji po hlášení odstranil provozovatel.',
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
  'read.unknown': 'Zpráva neexistuje nebo ji už nejde otevřít.',
  'read.retry_later': 'Služba je přetížená. Zkuste to za {time}.',
  'read.ended.opened': 'Zpráva už byla otevřena.',
  'read.ended.expired': 'Platnost zprávy vypršela.',
  'read.ended.cancelled': 'Odesílatel zprávu zrušil.',
  'read.ended.deleted': 'Zpráva byla smazána.',
  'read.passphrase': 'Passphrase',
  'read.reveal': 'Zobrazit zprávu',
  'read.resumable': 'Zprávu jste na této stránce už otevřeli a můžete ji načíst znovu, dokud běží lhůta.',
  'read.lease_ended': 'Čas na opětovné načtení zprávy na této stránce vypršel. Zobrazení teď spotřebuje další otevření.',
  'read.cleared': 'Stránku jste opustili, a tak už zprávu nezobrazuje.',
  'read.device': 'Minule se odvození klíče na tomto zařízení nedokončilo. Pokud se to opakuje, otevřete odkaz na jiném zařízení.',
  'read.rejected.wrong': 'Passphrase nesedí. Zbývající pokusy bez čekání: {count}.',
  'read.rejected.wait': 'Passphrase nesedí. Další pokus je možný za {time}.',
  'read.rejected.malformed': 'Passphrase obsahuje neplatné znaky.',
  'read.rejected.wrong_or_changed': 'Zprávu se nepodařilo dešifrovat: passphrase nesedí, nebo se data změnila. Zadejte passphrase znovu.',
  'read.delete': 'Smazat bez zobrazení',
  'read.delete.rest': 'Smazat zbývající otevření',
  'read.delete.confirm': 'Zprávu pak už nikdo neotevře, ani vy.',
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
  'revealed.confirm.title': 'Tento typ souboru může spustit kód. Otevřete ho, jen pokud odesílateli věříte.',
  'revealed.confirm.extension': 'Přípona souboru: .{extension}',
  'revealed.confirm.none': 'Soubor nemá příponu.',
  'revealed.confirm.download': 'Přesto stáhnout',
  'revealed.confirm.cancel': 'Nestahovat',

  'report.title': 'Nahlásit zneužití',
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
  'report.problem.link_newer': 'Odkaz potřebuje novější verzi Nylzo.',
  'report.problem.link_foreign': 'Odkaz patří jiné službě.',
  'report.problem.reason_too_long': 'Důvod je delší než {bytes} bajtů.',

  'transparency.title': 'Transparentnost',
  'transparency.loading': 'Načítám údaje o službě…',
  'transparency.failed': 'Údaje o službě se nepodařilo načíst.',
  'transparency.intro': 'Co server této instance o zprávách ví, co ukládá a jak dlouho. Verzi, limity, provozovatele a otisky souborů posílá server ze své konfigurace. Popis toho, co ukládá a loguje, platí pro nezměněný server nylzo.',
  'transparency.operator.title': 'Provozovatel',
  'transparency.operator.name': 'Provozovatel',
  'transparency.operator.contact': 'Kontakt',
  'transparency.operator.hosting': 'Hosting',
  'transparency.operator.missing': 'Provozovatel tyto údaje neuvedl.',
  'transparency.stores.title': 'Co server ukládá',
  'transparency.stores.content': 'Zašifrovanou zprávu. Klíč je jen v části odkazu za #, kterou prohlížeč na server nikdy neposílá, takže server obsah ani název přílohy nepřečte.',
  'transparency.stores.metadata': 'U každé zprávy id z odkazu, zda ji chrání passphrase, počet otevření a chybných passphrase, platnost, zarovnanou velikost a časy zaokrouhlené na minutu. Během otevírání navíc záznam o rozpracovaném stažení, dokud neskončí.',
  'transparency.stores.keys': 'Místo klíčů jen jejich otisky (HMAC s tajemstvím serveru), které bez odkazu nikomu nepomohou.',
  'transparency.stores.ended': 'Zprávu smaže po posledním otevření, smazání příjemcem, zrušení, vypršení nebo odstranění provozovatelem po hlášení. Pak drží jen záznam o tom, jak skončila, nejdéle {days}.',
  'transparency.stores.abuse': 'Hlášení zneužití: datum, id zprávy a důvod bez odkazů, nejdéle {days}.',
  'transparency.stores.counters': 'Celkové denní počty vytvořených zpráv, bez adres, ještě {days} po konci dne.',
  'transparency.stores.addresses': 'IP adresy neukládá. Limity na adresu drží jen v paměti a po restartu je zapomene.',
  'transparency.logs.title': 'Co server loguje',
  'transparency.logs.what': 'Čas zkrácený na minutu, metodu, typ cesty, stav odpovědi a dobu vyřízení. U vytvoření, otevření a stažení zprávy dobu vyřízení vynechá, protože by prozradila velikost zprávy.',
  'transparency.logs.never': 'Nikdy id zprávy, IP adresu, klíče, podpisy, těla požadavků ani velikost zprávy.',
  'transparency.others.title': 'Co vidí ostatní',
  'transparency.others.hosting': 'Poskytovatel hostingu a proxy, která ukončuje HTTPS, vidí IP adresy, časy a velikosti požadavků a logují podle svých pravidel. Obsah zprávy nevidí.',
  'transparency.others.client': 'Kdo ovládá server nebo hosting, může poslat upravený webový klient a přečíst zprávy, které v něm vytvoříte nebo otevřete. Proti tomu chrání CLI z podepsaného vydání. Kontrola níže takovou záměnu odhalí, jen když se jí server nevyhýbá.',
  'transparency.limits.title': 'Limity této instance',
  'transparency.limits.file': 'Největší příloha',
  'transparency.limits.ttl': 'Nejdelší platnost',
  'transparency.limits.opens': 'Nejvíc otevření',
  'transparency.limits.create': 'Nové zprávy z jedné adresy',
  'transparency.limits.create.value': '{hour} za hodinu, {day} za den',
  'transparency.limits.read': 'Čtení z jedné adresy',
  'transparency.limits.read.value': '{hour} požadavků za hodinu',
  'transparency.limits.global': 'Nové zprávy celkem za den',
  'transparency.limits.global.value': '{max}, od poloviny s výpočtem proti zahlcení služby',
  'transparency.limits.none': 'bez limitu',
  'transparency.client.title': 'Verze a otisk webového klienta',
  'transparency.client.version': 'Verze serveru',
  'transparency.client.files': 'SHA-256 souborů webového klienta. U stránky jde o šablonu, do které server vkládá nonce.',
  'transparency.client.verify': 'Příkaz z vydání nylzo stejné verze ověří, že server posílá právě tyto soubory se stejnými hlavičkami:',
  'transparency.client.limit': 'Ověří jen to, co server poslal tomuto příkazu. Server ho může poznat a prohlížečům posílat jiný kód, takže kontrola odhalí chybné sestavení nebo nasazení, ne záměnu, kterou server úmyslně skrývá.',
  'transparency.source': 'Zdrojový kód, specifikace protokolu a model hrozeb',
} as const;

const cs = { ...baseCs, ...copyCs, ...homeCs };

export type MessageKey = keyof typeof cs;

const baseEn: Record<keyof typeof baseCs, string> = {
  'action.retry': 'Try again',
  'app.crashed': 'The page ran into an error. Reload it.',

  'nav.label': 'Main navigation',

  'create.title': 'New message',
  'create.text': 'Message text',
  'create.file.clear': 'Remove attachment',
  'create.passphrase': 'Passphrase, optional',
  'create.passphrase.hint': 'At least {min} characters. Send it to the recipient through another channel than the link.',
  'create.passphrase.weak': 'We recommend at least {words} random words and {chars} characters.',
  'create.submit': 'Create link',

  'ttl.3600': '1 hour',
  'ttl.86400': '24 hours',
  'ttl.259200': '3 days',
  'ttl.604800': '7 days',


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
  'failure.kdf': 'This device could not derive the key from the passphrase. Try another device.',
  'failure.pow': 'The anti-flooding check did not finish in time. Try again.',
  'failure.failed': 'The message could not be created.',

  'created.title': 'The link is ready',
  'created.link': 'Link to the message',
  'created.copy': 'Copy link',
  'created.copied': 'The link is on the clipboard',
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
  'sent.state.deleted': 'Deleted by a recipient, or removed by the operator after a report.',
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
  'read.unknown': 'The message does not exist or can no longer be opened.',
  'read.retry_later': 'The service is busy. Try again in {time}.',
  'read.ended.opened': 'The message has already been opened.',
  'read.ended.expired': 'The message has expired.',
  'read.ended.cancelled': 'The sender cancelled the message.',
  'read.ended.deleted': 'The message was deleted.',
  'read.passphrase': 'Passphrase',
  'read.reveal': 'Show message',
  'read.resumable': 'You already opened the message on this page and can load it again while the lease runs.',
  'read.lease_ended': 'The time to load the message again on this page ran out. Showing it now uses another open.',
  'read.cleared': 'You left the page, so it no longer shows the message.',
  'read.device': 'The key derivation did not finish on this device last time. If it happens again, open the link on another device.',
  'read.rejected.wrong': 'The passphrase does not match. Attempts left without waiting: {count}.',
  'read.rejected.wait': 'The passphrase does not match. The next attempt is possible in {time}.',
  'read.rejected.malformed': 'The passphrase contains invalid characters.',
  'read.rejected.wrong_or_changed': 'The message could not be decrypted: the passphrase does not match or the data changed. Enter the passphrase again.',
  'read.delete': 'Delete without showing',
  'read.delete.rest': 'Delete the remaining opens',
  'read.delete.confirm': 'After that nobody can open it, you included.',
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
  'revealed.confirm.title': 'This type of file can run code. Open it only if you trust the sender.',
  'revealed.confirm.extension': 'File extension: .{extension}',
  'revealed.confirm.none': 'The file has no extension.',
  'revealed.confirm.download': 'Download anyway',
  'revealed.confirm.cancel': 'Do not download',

  'report.title': 'Report abuse',
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
  'report.problem.link_newer': 'The link needs a newer version of Nylzo.',
  'report.problem.link_foreign': 'The link belongs to another service.',
  'report.problem.reason_too_long': 'The reason is longer than {bytes} bytes.',

  'transparency.title': 'Transparency',
  'transparency.loading': 'Loading the service details…',
  'transparency.failed': 'The service details could not be loaded.',
  'transparency.intro': 'What the server of this instance knows about messages, what it stores and for how long. The version, limits, operator and file fingerprints come from the server configuration. The description of what it stores and logs holds for an unmodified nylzo server.',
  'transparency.operator.title': 'Operator',
  'transparency.operator.name': 'Operator',
  'transparency.operator.contact': 'Contact',
  'transparency.operator.hosting': 'Hosting',
  'transparency.operator.missing': 'The operator has not provided these details.',
  'transparency.stores.title': 'What the server stores',
  'transparency.stores.content': 'The encrypted message. The key is only in the part of the link after #, which the browser never sends to the server, so the server cannot read the content or the name of an attachment.',
  'transparency.stores.metadata': 'For each message, the id from the link, whether a passphrase protects it, the number of opens and of wrong passphrases, the expiry, the padded size and times rounded to the minute. While a message is being opened, also a record of the download in progress until it ends.',
  'transparency.stores.keys': 'Instead of keys, only their fingerprints (HMAC with a server secret), which help no one without the link.',
  'transparency.stores.ended': 'It deletes a message after the last open, deletion by a recipient, cancellation, expiry or removal by the operator after a report. After that it keeps only a record of how the message ended, for at most {days}.',
  'transparency.stores.abuse': 'Abuse reports: the date, the message id and the reason without links, for at most {days}.',
  'transparency.stores.counters': 'Daily totals of created messages, without addresses, for {days} after the day ends.',
  'transparency.stores.addresses': 'It does not store IP addresses. Per-address limits live only in memory and are forgotten on restart.',
  'transparency.logs.title': 'What the server logs',
  'transparency.logs.what': 'The time truncated to the minute, the method, the kind of path, the response status and how long the request took. For creating, opening and downloading a message it leaves out the duration, because it would reveal the size of the message.',
  'transparency.logs.never': 'Never a message id, an IP address, keys, signatures, request bodies or the size of a message.',
  'transparency.others.title': 'What others see',
  'transparency.others.hosting': 'The hosting provider and the proxy that terminates HTTPS see IP addresses, request times and sizes, and log by their own rules. They do not see the content of a message.',
  'transparency.others.client': 'Whoever controls the server or the hosting can send a modified web client and read the messages you create or open in it. The CLI from a signed release protects against this. The check below finds such a swap only when the server does not avoid it.',
  'transparency.limits.title': 'Limits of this instance',
  'transparency.limits.file': 'Largest attachment',
  'transparency.limits.ttl': 'Longest expiry',
  'transparency.limits.opens': 'Most opens',
  'transparency.limits.create': 'New messages from one address',
  'transparency.limits.create.value': '{hour} per hour, {day} per day',
  'transparency.limits.read': 'Reads from one address',
  'transparency.limits.read.value': '{hour} requests per hour',
  'transparency.limits.global': 'New messages in total per day',
  'transparency.limits.global.value': '{max}, with a proof of work from half of that',
  'transparency.limits.none': 'no limit',
  'transparency.client.title': 'Version and fingerprint of the web client',
  'transparency.client.version': 'Server version',
  'transparency.client.files': 'SHA-256 of the web client files. For the page it is the template into which the server inserts a nonce.',
  'transparency.client.verify': 'This command, from the nylzo release of the same version, checks that the server sends exactly these files with the same headers:',
  'transparency.client.limit': 'It checks only what the server sent to this command. A server can recognise the command and send browsers other code, so the check finds a broken build or deployment, not a swap the server hides on purpose.',
  'transparency.source': 'Source code, protocol specification and threat model',
};

const en: Record<MessageKey, string> = { ...baseEn, ...copyEn, ...homeEn };

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
