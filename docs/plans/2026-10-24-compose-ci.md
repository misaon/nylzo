# Plán: Compose s Caddy v CI

Datum: 24. 10. 2026. Větev: `claude/compose-ci` z `main`. Zdroj pravdy: AK12, AK17 a AK26 v design docu, spec §10 (co Caddy nesmí logovat) a `docs/plans/2026-10-23-release-pipeline.md`, podle kterého se příklad Compose ověřoval ručně a skripty zůstaly mimo repozitář. Stav: samostatná noční práce na pokyn vlastníka z 24. 10. 2026.

## D67: job „Compose with Caddy“

Nový job v `ci.yml` sestaví `nylzo` a image se stejným základem a rozložením jako image vydání (`/ko-app/nylzo` na distroless z `.goreleaser.yaml`), spustí `deploy/compose` beze změny s doménou `localhost`, pro kterou Caddy vydá certifikát své interní CA, a ověří:

- AK17: CLI vytvoří a přečte 5MiB soubor přes Caddy a HTTPS; obsah sedí bajt po bajtu.
- AK26: nástroj `tools/tlscheck` nabídne v TLS 1.3 X25519MLKEM768 i X25519 jako prohlížeč a vyžaduje, aby server zvolil hybridní skupinu. Systémové OpenSSL na Ubuntu 24.04 ML-KEM nezná, proto nástroj v Go.
- D61: `nylzo verify-client` přes Caddy najde stejného webového klienta, jakého má vložený, se stejnými hlavičkami. Ověří tím i to, že Caddy v příkladu nepřidává hlavičku, kterou by kontrola musela povolit.
- spec §10: když `nylzo` neběží a Caddy vrací 502, log Caddy (načtený po krátkém čekání, aby v něm byl i poslední řádek) neobsahuje id zprávy, cestu `/m/` ani hodnotu hlavičky `X-Nylzo-Lease`.
- AK12: s limitem tří požadavků na zdroj dostanou čtyři požadavky s různým podvrženým `X-Forwarded-For` přesně 200, 200, 200 a 429, takže podvržená hlavička nevytvoří vlastní kyblík. Požadavek ze druhého skutečného zdroje (přes IP adresu runneru místo localhostu) pak dostane 200, takže server bere adresu, kterou Caddy zapsal, a nesdílí jeden kyblík pro všechny.

Job neověřuje vydání samotné (image z registru, podpisy); to dělá `release:verify-image` a postup v `docs/release-checklist.md`. AK17 a AK26 na veřejné instanci zůstávají na nasazení.
