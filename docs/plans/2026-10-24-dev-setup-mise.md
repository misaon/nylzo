# Plán: vývojářské prostředí přes mise

Datum: 24. 10. 2026. Větev: `claude/mise` z `main` po sloučení PR 8 (`43376a0`). Stav: vlastník 24. 10. 2026 napsal, že spuštění front-endu je náročné, a navrhl integrovat do projektu nástroj mise, aby lokální běh byl jednoduchý i pro komunitu.

## Cíl

Kdo naklonuje repozitář a má mise, spustí Nylzo dvěma příkazy: `mise install` stáhne připnuté Go, Node.js a pnpm a `mise run dev` sestaví webového klienta, binárku a pustí server na `http://localhost:8080`. Front-end vývojář dostane i Vite s hot reloadem. Ruční postup bez mise v README zůstává.

Hotovo znamená: `mise.toml` a `mise.lock` v kořeni, úlohy pro vývoj, testy, lint a e2e, job v CI, který postup ověří na čistém stroji, a README s rychlým startem.

## Rozhodnutí

### D58: mise jako vstupní bod pro vývojáře

- `mise.toml` připíná Go 1.27.3, Node.js 24.22.1 a pnpm 12.14.0. `mise.lock` k nim drží adresu a SHA-256 pro linux-x64, linux-arm64, macos-x64 a macos-arm64. Ověřeno 24. 10. 2026 s mise 2026.10.9: `mise install` stažení s jiným checksumem, než má zámek, odmítne. Go a Node.js stahuje z dl.google.com a nodejs.org, pnpm z GitHub Releases přes backend aqua, který navíc ověří atestaci GitHubu.
- Verze dál stojí i v `go.mod` (`toolchain`), `web/.node-version` a poli `packageManager`, protože z nich čte CI a GoReleaser. Job „Developer setup (mise)“ v CI hlídá, že `mise.toml` říká totéž a že úloha `lint` volá stejnou verzi golangci-lint jako CI.
- `GOTOOLCHAIN=local` v prostředí mise zakáže Go stahovat jiný toolchain. Kvůli sekcím `[env]` a `[settings]` mise konfiguraci použije až po `mise trust` (ověřeno s 2026.10.9); README to uvádí jako první krok, aby si ji přispěvatel před povolením přečetl.
- Úlohy: `install`, `build:web`, `build`, `dev`, `dev:web`, `dev:reset`, `test`, `lint`, `e2e:browsers`, `e2e`. Spouštějí stejné příkazy jako CI, aby lokální výsledek odpovídal CI.
- `mise run dev` naslouchá jen na `127.0.0.1:8080` (port změní `NYLZO_LISTEN`). Pepper vygeneruje z `/dev/urandom` do `.dev/pepper` s právy 0600 a data dává do `.dev/data`, takže zprávy přežijí restart. Limity vytváření a čtení vypne, aby opakované zkoušení nenarazilo na 20 zpráv za hodinu; PoW, velikosti a ostatní limity zůstávají jako v produkci. `mise run dev:reset` adresář `.dev` smaže. `.dev/` a `bin/` jsou v `.gitignore`.
- Úlohy potřebují POSIX shell, tedy Linux, macOS nebo WSL. Server na Windows stejně neběží (D54).

Cena: přispěvatel, který mise nechce, má o jeden nástroj víc v návodu, proto ruční postup zůstává. mise sám je další místo důvěry, protože stahuje nástroje; zámek s checksumy a v CI akce připnutá na SHA s pevnou verzí mise, jejíž stažení akce ověří proti podepsaným checksumům (minisign), ho omezují.

### D59: Vite s hot reloadem proti lokálnímu serveru

`mise run dev:web` pustí Vite na `http://localhost:5173` a požadavky na `/api` přepošle serveru z `mise run dev` (jinou adresu zadá `NYLZO_DEV_API`). Proxy je jen v konfiguraci vývojového serveru Vite a produkční sestavení nemění; ověřeno 24. 10. 2026 vytvořením zprávy v prohlížeči a 2MB přílohou tam i zpět přes CLI. Vite posílá stránky bez CSP a hlaviček serveru, takže CSP, nonce a Trusted Types ověří jen `mise run dev` a e2e testy. Vite naslouchá jen na localhostu; síťová adresa by stejně nefungovala, protože Web Crypto potřebuje HTTPS nebo localhost.

### CI

Nový job „Developer setup (mise)“: `jdx/mise-action` v5.2.0 připnutý na SHA s mise 2026.10.9 a bez cache, aby PR do cache nezapisovaly. Nainstaluje nástroje přesně podle zámku (`--locked`), porovná verze se soubory, ze kterých čte zbytek CI, spustí `mise run build`, pustí `mise run dev` a ověří, že `/healthz` odpovídá a `/` servíruje sestaveného klienta, ne zástupnou stránku. Ostatní joby se nemění.

## Mimo plán

- Převod ostatních jobů CI na úlohy mise. Šel by, ale vyměnil by ověřené akce `setup-go` a `setup-node` a jejich cache; vrátíme se k tomu, pokud se pravidla v `mise.toml` a CI začnou rozcházet.
- Aktualizace mise na novější verzi; `min_version` drží verzi, se kterou je postup ověřený.
