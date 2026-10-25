# Product Marketing Context

**Document version:** v1
**Last updated:** 2026-10-25

## Product Overview
**One-liner:** Jednorázový šifrovaný odkaz na heslo, klíč nebo soubor, který server nedokáže přečíst.
**What it does:** Odesílatel napíše text nebo přiloží soubor do 5 MB, prohlížeč (nebo CLI) ho zašifruje a server dostane jen šifru. Příjemce dostane odkaz, který se otevře jen tolikrát, kolikrát odesílatel určil (výchozí jednou), a po posledním otevření nebo vypršení platnosti server šifru smaže. Klíč je v části odkazu za znakem #, kterou prohlížeč serveru neposílá.
**Product category:** Sdílení tajemství jednorázovým odkazem (one-time secret, burn after reading, encrypted paste). Lidé hledají „bezpečně poslat heslo“, „jednorázový odkaz“, „share password securely“, „one-time secret“.
**Product type:** Open-source webová služba (veřejná instance) a self-hosted server s CLI v jedné binárce.
**Business model:** Zdarma, open source pod licencí MIT. Veřejná instance bez účtu a bez placeného tarifu. Firmy si mohou Nylzo nasadit u sebe. Žádný placený produkt zatím neexistuje.

## Target Audience
**Target companies:** Vývojářské a IT týmy, firmy s vlastním provozem a bezpečnostními pravidly (self-hosting kvůli kontrole nad daty), menší firmy bez správce hesel pro externí předávání.
**Decision-makers:** Vedoucí IT, security engineer, DevOps nebo platform tým (pro self-hosting); jednotlivec rozhoduje sám (pro veřejnou instanci).
**Primary use case:** Poslat někomu heslo, API klíč, přístup k serveru nebo citlivý dokument tak, aby nezůstal v chatu, e-mailu nebo ticketu.
**Jobs to be done:**
- Předat tajemství jednomu člověku tak, aby ho po přečtení nikde nenašel někdo další.
- Mít jistotu, že ho po cestě nepřečte služba, přes kterou jde.
- Ve firmě nabídnout lidem schválený nástroj na předávání tajemství, který běží na vlastní infrastruktuře.
**Use cases:**
- Admin posílá kolegovi nebo dodavateli heslo k databázi nebo přístup k serveru.
- Vývojář předává API klíč nebo token mimo Slack.
- Běžný uživatel posílá rodině heslo k Wi-Fi nebo k účtu, nebo citlivý dokument (smlouvu, sken dokladu).
- AI agent předá tajemství odkazem přes CLI nebo skill, aby plaintext nezůstal v kontextu modelu.
- Firma nasadí vlastní instanci pro zaměstnance a dodavatele.

## Personas
| Persona | Cares about | Challenge | Value we promise |
|---------|-------------|-----------|------------------|
| Vývojář nebo admin (uživatel) | Rychlost, ověřitelnost, žádná registrace, CLI | Tajemství končí ve Slacku, v ticketu nebo v historii shellu | Odkaz za pár vteřin, šifruje se u odesílatele, otevřený kód a reprodukovatelný build |
| Security engineer (technický vlivník) | Model hrozeb, co server ukládá a loguje, dodavatelský řetězec | Neprůhledné SaaS nástroje, logy s IP a obsahem | Stránka Transparentnost, specifikace protokolu, test ukradeného serveru, podepsaná vydání |
| Vedoucí IT nebo platform tým (rozhoduje o self-hostingu) | Snadný provoz, kontrola nad daty, licence | Další služba na údržbu, závislosti, databáze | Jedna binárka se SQLite, Docker Compose s Caddy, licence MIT |
| Běžný uživatel | Aby to šlo snadno a bylo to bezpečné | Neví, jak heslo poslat bezpečně, kryptografii nerozumí | Napíše, klikne, pošle odkaz; po otevření zpráva zmizí |

## Problems & Pain Points
**Core problem:** Hesla a klíče se posílají chatem, e-mailem nebo ticketem, kde zůstanou v historii, zálohách a u poskytovatele služby navždy.
**Why alternatives fall short:**
- Chat a e-mail: tajemství zůstane v historii u odesílatele, příjemce i poskytovatele.
- Služby, které šifrují na serveru (OneTimeSecret ve výchozím stavu), vidí obsah.
- Náhledy odkazů ve Slacku, Teams nebo e-mailových skenerech u některých nástrojů zprávu spotřebují dřív, než ji otevře příjemce.
- U nástrojů, kde id zprávy stačí k jejímu stažení, stačí uniklé id.
- Bitwarden Send vyžaduje, aby měl odesílatel účet ve správci hesel.
**What it costs them:** Uniklé přístupy, rotace hesel po incidentu, porušení interních pravidel.
**Emotional tension:** „Neměl jsem to posílat do Slacku.“ Nejistota, kdo všechno zprávu viděl, a jestli ji neotevřel náhled odkazu.

## Competitive Landscape
**Direct:** PrivateBin, Yopass, OneTimeSecret, Bitwarden Send (srovnání v sekci Comparison k 24. 10. 2026, zdroje v docs/research/C-existing-tools.md).
**Secondary:** Správci hesel se sdílením (Bitwarden, 1Password), šifrované e-maily.
**Indirect:** Poslat heslo chatem, e-mailem nebo po částech dvěma kanály.

## Differentiation
**Key differentiators:**
- Šifruje se v prohlížeči nebo CLI, server dostane jen šifru a odkaz nese klíč za #.
- Náhled odkazu zprávu nespálí, otevření počítá až klik příjemce.
- Passphrase je součástí šifrování a server ji ověří, aniž by ji znal; pokusy omezuje.
- Samotné id zprávu neotevře: otevření a stažení vyžaduje podpis klíčem z odkazu.
- Šifra je zarovnaná na celé bloky (text po 4 KiB, soubor po 64 KiB), takže server pozná jen zaokrouhlenou velikost.
- Server neloguje IP adresy ani id zpráv; test s každou změnou kódu prohledá kopii serveru a nesmí najít klíče ani obsah.
- Reprodukovatelný build a podepsaná vydání (pipeline je hotová, první vydání zatím nevyšlo); `nylzo verify-client` porovná servírovaného klienta s kopií v binárce, záměnu, kterou server před ním schová, ale neodhalí.
- Jedna binárka se SQLite pro self-hosting.
**How we do it differently:** Bezpečnostní vlastnosti jsou ověřitelné: specifikace protokolu, testovací vektory, stránka Transparentnost s otisky souborů.
**Why that's better:** Nemusíte věřit tvrzení na webu, můžete si ho ověřit.
**Why customers choose us:** Zatím bez dat od zákazníků. Předpoklad: kombinace jednoduchosti (bez účtu) a ověřitelnosti.

## Objections
| Objection | Response |
|-----------|----------|
| „Proč bych měl věřit vašemu serveru?“ | Server dostane jen šifru; co ukládá a loguje, ukazuje Transparentnost; kód je otevřený a build reprodukovatelný. Upřímně: změněný server může poslat podvržený web, proto existuje `verify-client` a CLI. |
| „Co když odkaz někdo zachytí?“ | Kdo má odkaz první, otevře ho. Proto passphrase jiným kanálem; příjemce uvidí, že už byla zpráva otevřená, a odesílatel to vidí na stránce Odeslané. |
| „Nemá to audit.“ | Pravda, nezávislý audit zatím není a web to říká ve srovnání. |
| „Nechci další službu na provoz.“ | Jedna binárka se SQLite, nebo Compose s Caddy. |
| „Je to zdarma, kde je háček?“ | Open source pod MIT, bez účtu, bez cookies a analytiky. Veřejná instance má limity. |

**Anti-persona:** Kdo potřebuje trvalé úložiště nebo sdílení souborů nad 5 MB; kdo chce posílat podvodné nebo nelegální obsahy; kdo potřebuje garantovanou dostupnost veřejné instance (SLA).

## Switching Dynamics
**Push:** Tajemství zůstávají v chatu a e-mailu; incident nebo audit, který to odhalil; pravidla firmy.
**Pull:** Žádný účet, odkaz za pár vteřin, ověřitelná tvrzení, možnost provozovat u sebe.
**Habit:** „Pošlu to do Slacku a pak smažu.“ Zvyk na stávající nástroj (OneTimeSecret, PrivateBin).
**Anxiety:** Ztratí se zpráva, když ji otevře náhled? Co když příjemce odkaz neotevře včas? Je to dost bezpečné? Pochopí to příjemce?

## Customer Language
**How they describe the problem:**
- „Jak bezpečně poslat heslo?“ (předpoklad z vyhledávání, zatím bez rozhovorů se zákazníky)
- „Nechci to posílat do chatu.“
**How they describe us:**
- Zatím bez citací od uživatelů.
**Words to use:** odkaz, zpráva, otevření, platnost, šifra, klíč, passphrase (s vysvětlením), server, prohlížeč, smazat, zrušit.
**Words to avoid:** „zero knowledge“, „nevratně smazané“, „ani provozovatel nikdy nepřečte“, „nic neunikne“, „kvantově odolné“, „auditováno“, „garantovaná dostupnost“, „vojenská bezpečnost“ (vše tvrzení, která nejdou doložit; viz rozhodovací shrnutí v docs/designs/nylzo-architecture-research.md). Dále prázdná slova jako „revoluční“, „bezkonkurenční“, „robustní“, „seamless“.
**Glossary:**
| Term | Meaning |
|------|---------|
| Odkaz | Adresa zprávy; za # nese klíč, který server nevidí |
| Otevření | Jedno zobrazení zprávy; spotřebuje se až po kliknutí |
| Passphrase | Volitelné heslo k odkazu, posílá se jiným kanálem; čeština ponechává anglický termín |
| Šifra | Zašifrovaná data, která drží server (anglicky ciphertext) |
| Instance | Jeden běžící server Nylzo (veřejný nebo vlastní) |
| Transparentnost | Stránka, kde instance ukazuje, co ukládá, loguje a jaké má limity |

## Brand Voice
| Attribute | Means | Doesn't mean | Example line |
|-----------|-------|--------------|--------------|
| Věcný | Krátké věty, konkrétní čísla a mechanismy místo přídavných jmen | Suchý seznam funkcí bez přínosu | „Server dostane jen šifru a po otevření ji smaže.“ |
| Ověřitelný | Každé tvrzení jde doložit kódem, specifikací nebo stránkou Transparentnost | Přetížený kryptografickými detaily na první obrazovce | „Sliby, které si můžete ověřit“ |
| Upřímný o hranicích | Otevřeně říká, co Nylzo neochrání | Strašení nebo zpochybňování vlastního produktu | „Napadené zařízení žádná šifra neochrání“ |
| Klidný | Bez naléhavosti, bez umělého strachu | Lhostejný k riziku | „Máte něco, co nepatří do chatu?“ |

**Tone by context:**
- Support: odpověď nejdřív, pak krok, který uživatel může udělat.
- Launch: co je nové, pro koho, jedno konkrétní číslo.
- Errors: jedna věta, co se stalo, jedna věta, co dělat dál. Bez omlouvání a bez vtipů.
- Sales: jako jeden admin druhému; mechanismus a důkaz, žádný hype.
- Social: názor a konkrétní příklad, stále bez nedoložitelných tvrzení.
**Terms we own:** „Sliby, které si můžete ověřit“, test ukradeného serveru, Transparentnost.
**Never use (and why):** Viz Words to avoid; každé z nich buď nejde doložit, nebo je nepravdivé pro změněný server nebo klienta.

## Proof Points
**Metrics:** Žádné metriky používání zatím nejsou. Technická fakta (ověřitelná v kódu a specifikaci): klíč 256 bitů, id 128 bitů, AES-256-GCM, scrypt pro passphrase, nejvýše 100 špatných pokusů o passphrase za život zprávy, bloky 4 KiB (text) a 64 KiB (soubor), přílohy do 5 MB, platnost nejvýše 7 dní, ve výchozím stavu nejvýš 10 otevření.
**Customers:** Zatím žádní uvedení zákazníci.
**Testimonials:** Žádné.
**Value themes:**
| Theme | Proof |
|-------|-------|
| Server obsah nepřečte | Šifrování v klientovi, klíč za #, specifikace spec/protocol.md |
| Náhled nespálí zprávu | Otevření až po POST z kliknutí (spec, e2e testy) |
| Server o vás neví víc, než musí | Logy bez IP a id, stránka Transparentnost, test ukradeného serveru |
| Ověřitelnost | Otevřený kód MIT, reprodukovatelný build (CI, AK15), podepsaná vydání od prvního vydání, verify-client |
| Snadný vlastní provoz | Jedna binárka se SQLite, deploy/compose s Caddy |

## Goals
**Business goal:** Používání veřejné instance a nasazení u firem (self-hosting).
**Conversion action:** Primárně vytvořit odkaz na domovské stránce (bez účtu). Sekundárně přejít k self-hostingu (sekce Self-hosting, GitHub).
**Current metrics:** Neměří se; web nemá analytiku (záměrně).

## Learnings
*Decisions and tested lessons. Newest first. Keep it to about 20 active entries.*
| Date | Learning | Evidence | Applies to | Status |
|------|----------|----------|------------|--------|
| 2026-10-25 | Každé tvrzení na webu musí sedět se specifikací a kódem; faktická kontrola 25. 10. našla a opravila mimo jiné „jen bloky 4 KiB“, „server uloží jen šifru“ a „verify-client to ověří“. | Kontrola textů proti spec/protocol.md a kódu | Všechny texty | active |
| 2026-10-25 | Web nesmí používat tvrzení ze seznamu Words to avoid. | Rozhodovací shrnutí v docs/designs/nylzo-architecture-research.md, tři nezávislé kontroly | Všechny texty | active |
| 2026-10-25 | Publikum jsou vývojáři a admini, firmy a týmy (IT, security) i běžní uživatelé; hlavní akce je vytvořit odkaz, druhá self-hosting. | Rozhodnutí vlastníka | Domovská stránka, SEO | active |

## Changelog
*Newest first. One line per revision: what changed and why.*
- v1 (2026-10-25) — Initial context, auto-drafted from the repository and the owner's answers on goal and audience.
