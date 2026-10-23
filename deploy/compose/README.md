# Self-host přes Docker Compose

Příklad spustí `nylzod` za Caddy, který na stejném stroji ukončuje TLS (HTTP/2, HTTP/3 a post-kvantová výměna klíčů X25519MLKEM768). Kdo ukončuje TLS, vidí klíče R, V, M a L v provozu a může podvrhnout klienta, proto TLS nikdy neukončujte u cizí CDN nebo proxy.

Potřebujete Docker Compose v2, doménu s DNS záznamem na tento stroj a otevřené porty 80/tcp, 443/tcp a 443/udp.

## Spuštění

1. Ověřte image a zjistěte jeho digest. `gh attestation verify` kontroluje provenance z workflow vydání, `cosign verify` podpis:

   ```bash
   gh attestation verify oci://ghcr.io/misaon/nylzo:0.1.0 --repo misaon/nylzo
   cosign verify ghcr.io/misaon/nylzo:0.1.0 --certificate-oidc-issuer https://token.actions.githubusercontent.com --certificate-identity https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/v0.1.0
   ```

2. Vytvořte `.env` podle `.env.example`. Do `NYLZO_IMAGE` dejte ověřenou referenci včetně `@sha256:…`, aby Compose nikdy nestáhl jiný image pod stejným tagem.

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

- `nylzod` má kořenový systém souborů jen pro čtení, žádné capabilities a `no-new-privileges`. Zapisuje jen do volume `nylzo-data`.
- Pepper předává Docker secret jako soubor (`NYLZO_PEPPER_FILE`), takže není v prostředí procesu ani ve výpisu `docker inspect`.
- `NYLZO_TRUSTED_PROXIES` je síť Compose, aby server bral adresu klienta z hlavičky `X-Forwarded-For` od Caddy. Když síť `172.30.80.0/24` koliduje s jinou, změňte ji na obou místech v `compose.yaml`.
- Caddy nezapisuje access log a skrývá hlavičku `Server`. Bezpečnostní hlavičky a CSP nastavuje `nylzod`.

## Rotace pepperu a aktualizace

- Rotace: starý pepper přesuňte do souboru, přidejte secret pro `NYLZO_PEPPER_PREVIOUS_FILE` a vygenerujte nový pepper. Starý nechte 14 dní a jeden interval úklidu (README v kořeni repozitáře).
- Aktualizace: ověřte nový image podle kroku 1, změňte `NYLZO_IMAGE` a spusťte `docker compose up -d`.
- Zálohy: obsah se nezálohuje (OD20). Zálohujte jen `.env` a pepper, odděleně od dat.
