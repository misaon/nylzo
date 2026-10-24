# Plán: milník 7, MCP server

Datum: 24. 10. 2026. Stav: návrh čeká na rozhodnutí vlastníka OD-M1 až OD-M5; kód zatím nevznikl. Zdroj: `docs/designs/nylzo-architecture-research.md` (B6, sekce „CLI, MCP a budoucí skill“ bod 4 a tabulka „Kde tajemství může zůstat“), skill `skills/nylzo` (D70) a milník 4 (`docs/plans/2026-10-23-milestone-4-cli.md`).

## Cíl

Agent, který umí MCP, pošle a otevře tajemství nástrojem, ne příkazem v shellu, a přitom platí stejná pravidla jako u CLI: tajemství, odkaz, passphrase ani správcovský token nejsou v parametrech nástroje, pokud to uživatel výslovně nechce, a dešifrovaný obsah jde do souboru s právy 0600, ne do výsledku nástroje.

Nástroje podle designu: `create_secret` (text nebo cesta k souboru, ttl, opens, cesta k souboru s passphrase; výstup URL a volitelně M), `read_secret` (cesta k souboru s odkazem, odkaz v parametru jen s `allow_inline_link: true`; passphrase jen cestou k souboru nebo elicitation v URL režimu; výsledek je cesta k souboru 0600 nebo `resource_link`, plaintext jen s `inline: true`), `status` a `cancel` (M cestou k souboru). Parametry s tajemstvím nikdy nenesou `x-mcp-header`, passphrase nikdy nejde přes elicitation ve formulářovém režimu a server nikdy neloguje obsah ani odkaz.

Návrh se od designu liší ve dvou bodech, oba jako u skillu (D70): `create_secret` zapíše odkaz a M do souborů v soukromém adresáři a vrátí cesty, URL vrátí jen na výslovné přání, protože odkaz ve výsledku nástroje zůstane v přepisu; a text tajemství v parametru je OD-M5.

## Co je ověřené (24. 10. 2026)

- Aktuální revize MCP je `2026-07-28` (modelcontextprotocol.io/specification/versioning a changelog revize). Zrušila handshake `initialize`: každý požadavek nese verzi v `_meta` pod klíčem `io.modelcontextprotocol/protocolVersion`, server odpoví `UnsupportedProtocolVersionError` (kód -32022) a musí umět `server/discover`. Revize do `2025-11-25` handshake mají, takže server, který chce mluvit i s klienty bez nové revize, musí umět obě generace.
- Oficiální Go SDK `github.com/modelcontextprotocol/go-sdk` má poslední stabilní verzi v1.8.0 z 4. 9. 2026 (pkg.go.dev a proxy.golang.org; vydání na GitHubu nese datum 14. 9. 2026). Podle poznámek k vydání je `2026-07-28` nejnovější revize, kterou vyjednává. Jeho `go.mod` vyžaduje osm modulů: `golang-jwt/jwt/v5`, `google/go-cmp`, `google/jsonschema-go`, `segmentio/encoding`, `yosida95/uritemplate/v3`, `golang.org/x/oauth2`, `golang.org/x/time`, `golang.org/x/tools`, k tomu nepřímo `segmentio/asm`, `x/sync` a `x/sys`.
- Podle zdrojového kódu v1.8.0 (čteno, nespuštěno): výsledek `resource_link` SDK umí (`mcp/content.go`). Elicitation v URL režimu umí jen pro sessions starších revizí (`mcp/protocol.go`, `mcp/client.go`, `mcp/server.go`); v revizi `2026-07-28` `ServerSession.Elicit` vrátí chybu a nástroj místo toho vrací `InputRequiredResult` se žádostmi o vstup (SEP-2322). Revize `2026-07-28` zároveň odstranila `elicitationId` a `notifications/elicitation/complete`.

## Rozhodnutí pro vlastníka

- OD-M1 knihovna. (a) Oficiální SDK v1.8.0: hotová podpora obou generací protokolu a údržba od autorů specifikace, za cenu jedenácti modulů v grafu závislostí, z toho JWT a OAuth, které stdio server nepotřebuje. (b) Vlastní minimální server nad standardní knihovnou: jen JSON-RPC přes stdio, `tools/list` a `tools/call` pro obě generace protokolu, žádná nová závislost, ale každou novou revizi MCP musíme sami dohnat. Doporučení: (a), protože revize `2026-07-28` ukázala, že protokol se mění zásadně, a ruční implementace by zastarávala nejrychleji.
- OD-M2 binárka. (a) Samostatná `nylzo-mcp`, jak říká design, a navíc ve vlastním modulu Go (`mcp/go.mod`): závislosti SDK se nedostanou do binárky serveru ani do jejího `go.sum`. (b) Příkaz `nylzo mcp` v jedné binárce podle D53: jednodušší distribuce, ale SDK by bylo i v serveru. Doporučení: (a) při OD-M1 (a), (b) při OD-M1 (b).
- OD-M3 passphrase. V první verzi jen cesta k souboru. Elicitation v URL režimu potřebuje stránku, na kterou uživatel passphrase zadá, tedy lokální HTTP server v `nylzo-mcp`, a v revizi `2026-07-28` se dělá jinak než ve starších. Doporučení: soubor teď, URL režim až samostatně pro obě generace.
- OD-M5 text tajemství v parametru `create_secret`. Design ho dovoluje, ale text pak leží v kontextu modelu i v přepisu. (a) Jen cesta k souboru; (b) text v parametru jen s výslovným `allow_inline_secret: true`; (c) jak v designu. Doporučení: (b), stejně jako `allow_inline_link` u čtení.
- OD-M4 vydání. `nylzo-mcp` ve stejném vydání jako `nylzo` (GoReleaser, podpisy, SBOM a atestace jako dnes), nebo až v samostatném vydání. Doporučení: stejné vydání, aby uživatel ověřoval jeden `checksums.txt`.

## Postup po rozhodnutí

1. Specifikace nástrojů v `spec/mcp.md`: vstupy, výstupy, chyby a co nikdy nejde do výsledku.
2. Server nad sdíleným `internal/client` a `internal/core`; žádné tajemství v parametru bez výslovného přepínače, výstupy v souborech 0600 v soukromém adresáři, stejné hlášky bez hodnot parametrů jako CLI.
3. Testy: každý nástroj proti `httptest` serveru, test, že odkaz, passphrase ani obsah nejsou ve výsledku ani na stderr, mutační kontrola těchto pravidel, e2e s Claude Code jako klientem podle možností CI.
4. Skill `skills/nylzo` doplní variantu s MCP.
