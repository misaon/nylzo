# Plán: úvodní stránka pro vyhledávače

Datum: 24. 10. 2026. Větev: `feat/seo-home-indexing`. Stav: vlastník 24. 10. 2026 rozhodl, že úvodní stránku pustí robotům, a nechal pro projekt nainstalovat skill `seo-audit`.

## Rozhodnutí

D80: vyhledávače smějí indexovat jen úvodní stránku. Server na `GET /` neposílá `X-Robots-Tag`; každá jiná odpověď ho dál nese s hodnotou `noindex, nofollow`: stránky `/m/…`, `/sent`, `/report` a `/transparency`, API, assety i náhled pro aplikace. Webový klient už nemá v `index.html` značku `<meta name="robots">`, protože tentýž soubor obsluhuje všechny stránky a cestu rozliší jen hlavička ze serveru. Zástupná stránka bez sestaveného klienta (`internal/server/web/placeholder.html`) zůstává `noindex`.

Soubor `robots.txt` server dál nemá (odpovídá 404, tedy bez omezení). Kdyby `robots.txt` zakázal `/m/`, vyhledávač by stránku nenačetl, hlavičku `noindex` by neviděl a adresu zprávy by mohl zařadit do indexu i bez obsahu.

Na bezpečnosti se nic nemění: klíč je ve fragmentu odkazu, který prohlížeč neposílá na server a vyhledávač ho nevidí. Stránka zprávy zůstává `noindex`.

## Co se mění

- `spec/protocol.md` §9: výjimka z `X-Robots-Tag` pro `GET /`.
- `docs/designs/nylzo-architecture-research.md`: hlavičky a AK10 s odkazem na D80.
- `internal/server/middleware.go` a `server.SecurityHeaders(path)`, ze kterého čte i `nylzo verify-client`, takže obě strany mají stejné pravidlo; testy hlaviček v Go i v e2e.
- `web/app/index.html` bez `<meta name="robots">`.

## Další kroky

Skill `seo-audit` (`.claude/skills/seo-audit`, verze ze `skills-lock.json`) se hodí na audit úvodní stránky, až bude veřejná instance. Kanonickou adresu a sitemap tato změna nepřidává, protože každá instance běží na vlastní doméně a server ji z hlavičky `Host` spolehlivě nezjistí.
