# Postup vydání

Pro maintainera, který vydává novou verzi Nylzo. Proč jednotlivé kroky existují, popisují D43, D52 a D64.

## Jednou předem (vlastník)

- Prostředí `release` v nastavení repozitáře s povinným schválením.
- Proměnná repozitáře `RELEASE_TAGGER_EMAIL` s e-mailem, kterým se podepisují tagy, a ověřený podpisový klíč na GitHubu.
- Ochrana větve `main` s povinnou revizí a zákaz přepisu tagů `v*`.
- Po prvním vydání zveřejnit balíček `ghcr.io/misaon/nylzo`.

## Vydání

1. Na `main` je poslední commit zelený v CI.
2. Podepsaný anotovaný tag a jeho push:

   ```bash
   git tag -s v0.1.0 -m "nylzo 0.1.0"
   ```

   ```bash
   git push origin v0.1.0
   ```

3. V záložce Actions schválit job `release` v prostředí `release`.

## Kontrola po vydání

1. Na vlastním stroji s mise sestavit stejný tag a porovnat ho s vydáním. Úloha skončí chybou, když se liší jediný bajt archivu nebo souboru webového klienta:

   ```bash
   git fetch --tags && git checkout v0.1.0
   ```

   ```bash
   mise install && mise run release:verify
   ```

2. Porovnat binárky v image s lokálním sestavením (potřebuje Docker):

   ```bash
   mise run release:verify-image
   ```

3. Když obojí sedí, připsat do poznámek vydání, kdo ho sestavil znovu, na jakém systému a SHA-256 publikovaného `checksums.txt`, který `release:verify` vypíše na konci, třeba „Maintainer rebuild on macOS arm64: checksums.txt sha256 …, identical, image binaries identical“. Když nesedí, vydání stáhnout a zjistit proč, nikdy ho nenechat viset.
4. Ověřit image podle `deploy/compose/README.md` (krok 1: `gh attestation verify` a `cosign verify`).
5. Po nasazení na veřejnou instanci spustit `nylzo verify-client --server https://…` z binárky téhož vydání.
