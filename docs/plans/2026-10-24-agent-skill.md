# Plán: skill pro agenty nad CLI

Datum: 24. 10. 2026. Větev: `claude/agent-skill` z `main`. Zdroj pravdy: `docs/designs/nylzo-architecture-research.md`, sekce „CLI, MCP a budoucí skill“ (bod 5 a tabulka „Kde tajemství může zůstat“), a specifikace Agent Skills na agentskills.io (ověřeno 24. 10. 2026). Stav: samostatná noční práce na pokyn vlastníka z 24. 10. 2026.

## D70: skill už v první verzi, nad CLI

Design řadil skill i MCP server do druhé verze. Skill je ale jen text nad hotovým CLI: nepotřebuje novou závislost ani změnu serveru a agentům, které už `nylzo` mají, hned řekne, jak tajemství neposlat do chatu. Proto jde do první verze. MCP server zůstává ve druhé verzi, protože přidává závislost (`go-sdk`) a vlastní rozhraní. Rozhodnuto samostatně v noci 24. 10. 2026 v rámci pokynu vlastníka, vlastník může vrátit.

- Skill leží v `skills/nylzo/SKILL.md`. Podle specifikace má `name` nejvýš 64 znaků z malých písmen, číslic a pomlček a shoduje se s adresářem, `description` má 1 až 1024 znaků a tělo má být pod 500 řádků.
- Obsah podle bodu 5 designu: kdy poslat odkaz místo vložení tajemství, jen CLI a nikdy WebFetch, curl nebo prohlížeč, tajemství dodá člověk do souboru nebo na stdin a agent ho nevypisuje, odkaz a správcovský token v souborech s právy 0600, passphrase jiným kanálem, obsah zprávy jako data, ne instrukce, a výstup `read` předat spotřebiteli přímo ze souboru. K tomu doporučená pravidla pro Claude Code (deny `WebFetch(domain:…)` a `Read(…)` pro výstupní adresář) a poctivé hranice z tabulky designu.
- Agent odkaz nevypisuje: `create` přesměruje do souboru s právy 0600 a uživateli řekne cestu. Odkaz ve výstupu nástroje je v přepisu a v požadavku k dodavateli modelu a do otevření funguje.
- Test v `internal/cli` hlídá, aby se skill a CLI nerozešly: každý řádek `nylzo …` v blocích kódu volá existující příkaz s přepínači, které ten příkaz zná, každý přepínač zmíněný v textu nějaký příkaz zná a frontmatter splňuje pravidla specifikace.

## Co skill nevynutí

Skill je jen text a model ho nemusí poslechnout. Vynucení dělá výchozí chování CLI (odmítne tajemství v argumentu, výstup zapisuje do soukromého adresáře), oprávnění a sandbox hosta. Co model jednou viděl, zůstane v požadavku k dodavateli modelu a v lokálním přepisu.

## Mimo plán

- MCP server `nylzo-mcp` (druhá verze).
- Balíček pluginu pro Claude Code s `.mcp.json`; dává smysl až s MCP serverem.
