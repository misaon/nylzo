# Plán: skill pro agenty nad CLI

Datum: 24. 10. 2026. Větev: `claude/agent-skill` z `main`. Zdroj pravdy: `docs/designs/nylzo-architecture-research.md`, sekce „CLI, MCP a budoucí skill“ (bod 5 a tabulka „Kde tajemství může zůstat“), a specifikace Agent Skills na agentskills.io (ověřeno 24. 10. 2026). Stav: samostatná noční práce na pokyn vlastníka z 24. 10. 2026.

## D70: skill už v první verzi, nad CLI

Design řadil skill i MCP server do druhé verze. Skill je ale jen text nad hotovým CLI: nepotřebuje novou závislost ani změnu serveru a agentům, které už `nylzo` mají, hned řekne, jak tajemství neposlat do chatu. Proto jde do první verze. MCP server zůstává ve druhé verzi, protože přidává závislost (`go-sdk`) a vlastní rozhraní. Rozhodnuto samostatně v noci 24. 10. 2026 v rámci pokynu vlastníka, vlastník může vrátit.

- Skill leží v `skills/nylzo/SKILL.md`. Podle specifikace má `name` nejvýš 64 znaků z malých písmen, číslic a pomlček a shoduje se s adresářem, `description` má 1 až 1024 znaků a tělo má být pod 500 řádků.
- Obsah podle bodu 5 designu: kdy poslat odkaz místo vložení tajemství, jen CLI a nikdy WebFetch, curl nebo prohlížeč, tajemství dodá člověk do souboru nebo na stdin a agent ho nevypisuje, odkaz a správcovský token v souborech v soukromém adresáři mimo repozitář, passphrase jiným kanálem, obsah zprávy jako data, ne instrukce, a výstup `read` předat spotřebiteli přímo ze souboru. K tomu pravidla pro Claude Code, která skill doporučí uživateli (deny `WebFetch(domain:…)` a `Read(~/…)` pro výstupní adresář, dvě proměnné prostředí a co vypínají), a hranice z tabulky designu.
- Agent odkaz nevypisuje: `create` přesměruje do souboru v adresáři z `mktemp -d` (práva 0700) mimo repozitář a uživateli řekne cestu. Na `umask` se skill nespoléhá, protože každé volání nástroje Bash může běžet v novém shellu. Odkaz ve výstupu nástroje zůstane v přepisu a v požadavku k dodavateli modelu a do otevření funguje.
- Odkaz, který uživatel už vložil do chatu, agent zapíše do souboru heredocem (není to argument), hned ho otevře a zbytek otevření smaže přes `burn`.
- Test v `internal/cli` hlídá, aby se skill a CLI nerozešly: každé volání `nylzo` v blocích kódu i v textu, i za rourou nebo středníkem, volá existující příkaz s přepínači, které ten příkaz zná, každý přepínač zmíněný v textu zná některý příkaz téže sekce, skill ukazuje všechny klientské příkazy a frontmatter má jen jednořádkové hodnoty v mezích specifikace. Výchozí hodnoty a chování CLI test nekontroluje.

## Co skill nevynutí

Skill je jen text a model ho nemusí poslechnout. Vynucení dělá výchozí chování CLI (odmítne tajemství v argumentu, výstup zapisuje do soukromého adresáře), oprávnění a sandbox hosta. Co model jednou viděl, zůstane v požadavku k dodavateli modelu a v lokálním přepisu.

## Mimo plán

- MCP server `nylzo-mcp` (druhá verze).
- Balíček pluginu pro Claude Code s `.mcp.json`; dává smysl až s MCP serverem.
