# Self-host přes Docker Compose

Příklad spustí `nylzo serve` za Caddy, který na stejném stroji ukončuje TLS (HTTP/2, HTTP/3 a post-kvantová výměna klíčů X25519MLKEM768). Kdo ukončuje TLS, vidí ciphertext, podpisy požadavků a lease a může podvrhnout webového klienta, proto TLS ukončujte jen u sebe nebo u hostingu, kterému svěřujete i samotný server, nikdy u další CDN nebo proxy.

Potřebujete Docker Compose v2, doménu s DNS záznamem na tento stroj a otevřené porty 80/tcp, 443/tcp a 443/udp.

## Spuštění

1. Zjistěte digest image a ověřte přímo tento digest, aby se mezi kontrolou a spuštěním nezměnil. `gh attestation verify` kontroluje provenance z workflow vydání a tagu `v0.1.0`, `cosign verify` podpis stejného workflow. Bez `--signer-workflow` by `gh` přijal provenance z kteréhokoli workflow v repozitáři:

   ```bash
   digest=$(docker buildx imagetools inspect ghcr.io/misaon/nylzo:0.1.0 --format '{{.Manifest.Digest}}')
   gh attestation verify "oci://ghcr.io/misaon/nylzo@${digest}" --repo misaon/nylzo --signer-workflow misaon/nylzo/.github/workflows/release.yml --source-ref refs/tags/v0.1.0 --deny-self-hosted-runners
   cosign verify "ghcr.io/misaon/nylzo@${digest}" --certificate-oidc-issuer https://token.actions.githubusercontent.com --certificate-identity https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/v0.1.0
   echo "NYLZO_IMAGE=ghcr.io/misaon/nylzo:0.1.0@${digest}"
   ```

2. Vytvořte `.env` podle `.env.example`. Do `NYLZO_IMAGE` dejte ověřenou referenci včetně `@sha256:…`, aby Compose nikdy nestáhl jiný image pod stejným tagem. `NYLZO_GLOBAL_DAILY_CREATES` nastavte podle disku (níže). `NYLZO_OPERATOR`, `NYLZO_CONTACT` (e-mail nebo adresa `https://`) a `NYLZO_HOSTING` uvidí návštěvníci na stránce `/transparency`; veřejná instance je potřebuje kvůli kontaktnímu místu podle DSA.

3. Vygenerujte pepper. Kontejner běží jako uživatel 65532, proto mu soubor patří a nikdo jiný ho nepřečte:

   ```bash
   mkdir -p secrets && openssl rand -hex 32 > secrets/pepper
   sudo chown 65532:65532 secrets/pepper && sudo chmod 0400 secrets/pepper
   ```

   Pepper do databáze nikdy nepatří. Když ho ztratíte, staré odkazy přestanou fungovat. Když unikne s kopií databáze, lze s ním ověřovat odhadnuté klíče (spec §13).

4. Spusťte:

   ```bash
   docker compose up -d
   ```

## Co příklad nastavuje

- `nylzo` má kořenový systém souborů jen pro čtení, žádné capabilities a `no-new-privileges`. Zapisuje jen do volume `nylzo-data`.
- Pepper předává Docker secret jako soubor (`NYLZO_PEPPER_FILE`), takže není v prostředí procesu ani ve výpisu `docker inspect`.
- Síť `nylzo` má IPv4 i IPv6 (ULA `fd1f:9791:e50b::/64`). Bez IPv6 by Docker posílal klienty IPv6 přes svou userland proxy, která se ke Caddy připojuje sama, a všichni klienti IPv6 by sdíleli jeden kyblík limitů. Docker proto musí mít zapnuté `ip6tables`, což je jeho výchozí stav.
- Caddy má pevné adresy `172.30.80.2` a `fd1f:9791:e50b::2` a `NYLZO_TRUSTED_PROXIES` obsahuje jen je. Server tak bere adresu klienta z `X-Forwarded-For` jen od Caddy, nikdy od brány sítě nebo jiného procesu na stroji. Caddy hlavičku od klienta zahodí a nastaví ji sám, takže podvržená hodnota nic nezmění. Ostatní kontejnery dostávají adresy z horní poloviny podsítí (`ip_range`), takže adresu Caddy nikdy nezaberou. Když podsítě kolidují s jinou sítí, změňte v `compose.yaml` podsítě, adresy Caddy i `NYLZO_TRUSTED_PROXIES`.
- Docker Desktop (macOS, Windows) přeposílá porty vlastní proxy, takže Caddy vidí u všech klientů adresu brány sítě a všichni sdílejí jeden kyblík limitů. Pro provoz proto použijte Linux s Docker Engine.
- `NYLZO_GLOBAL_DAILY_CREATES` je ve výchozím stavu 2000. Od 1000 vytvoření za den UTC server vyžaduje PoW, po 2000 vytvoření odpovídá 503 a čtení běží dál. Limity na adresu drží každý proces jen v paměti, takže proti útočníkovi, který střídá rozsahy adres, chrání disk až tento strop. Nastavte ho podle disku: zpráva má nejvýš asi 5,4 MB a žije nejvýš 7 dní, takže najednou může existovat až osm denních stropů zpráv, při 2000 kolem 86 GB. Když volné místo klesne pod `NYLZO_MIN_FREE_BYTES` (1 GiB), vytvoření odpoví 503 a burn, zrušení i čtení dál fungují.
- Caddy nezapisuje access log ani chyby s podrobnostmi požadavku. Globální volba `log default` vyřazuje loggery `http.log` (chyby a access log serveru), `http.handlers` (reverse proxy) a `http.stdlib`. Jinak by Caddy při 502, třeba během restartu `nylzo`, zapsal adresu klienta, cestu s id zprávy a hlavičky `X-Nylzo-*`. V logu zůstávají zprávy o startu a certifikátech.
- Caddy drží nečinné spojení na `nylzo` nejvýš 25 s. `nylzo` zavírá nečinná spojení po 30 s, takže Caddy nepošle požadavek do spojení, které server právě zavírá, a klient kvůli tomu nedostane 502.
- Caddy odstraní hlavičky `Server` a `Via` z odpovědí pro `NYLZO_DOMAIN`, i z chybových (například 502 při restartu `nylzo`) a z přesměrování z HTTP na HTTPS. Spojení s jiným hostem, třeba s holou IP adresou, ukončí bez odpovědi, takže neprozradí, jaký proxy server běží. Bezpečnostní hlavičky a CSP nastavuje `nylzo`.

## Rotace pepperu a aktualizace

- Rotace: starý pepper přesuňte do souboru, přidejte secret pro `NYLZO_PEPPER_PREVIOUS_FILE` a vygenerujte nový pepper. Starý nechte 14 dní a jeden interval úklidu (README v kořeni repozitáře).
- Aktualizace: ověřte nový image podle kroku 1, změňte `NYLZO_IMAGE` a spusťte `docker compose up -d`.
- Zálohy: obsah se nezálohuje (OD20). Zálohujte jen `.env` a pepper, odděleně od dat.
