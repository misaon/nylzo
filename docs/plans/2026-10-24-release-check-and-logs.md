# Plán: kontrola vydání maintainerem a rotace logů

Datum: 24. 10. 2026. Větev: `claude/ops`, postavená na `claude/takedown`. Zdroj pravdy: `docs/research/review-server-compromise.md` nález 9 (kontroly na straně zdroje, maintainer sestaví vydání lokálně a zveřejní svůj hash vedle hashe z CI), design „Distribution Plan“, `docs/plans/2026-10-23-release-pipeline.md` (bod „Postup zveřejnění vlastního hashe od maintainera zůstává v návrhu“) a rešerše `docs/research/I-hosting-prototype.md` (riziko 7, logy Dockeru bez limitu zaplní disk). Stav: samostatná noční práce na pokyn vlastníka z 24. 10. 2026.

## D64: maintainer sestaví vydání znovu a zveřejní výsledek

- GoReleaser 2.18.3 je v `mise.toml` a jeho adresy a checksumy v `mise.lock`, takže maintainer má stejnou verzi jako workflow vydání. Job „Developer setup (mise)“ hlídá, že verze sedí s `release.yml`.
- `mise run release:verify` na checkoutu tagu sestaví vydání bez publikace, podpisu, SBOM a image a porovná `checksums.txt` s tím, co je u vydání na GitHubu. `checksums.txt` pokrývá archivy binárek i soubory webového klienta.
- Ověřeno 24. 10. 2026 na macOS (arm64) s nástroji z `mise.lock`: snapshot commitu `5f403c1` dal stejné bajty všech čtyř binárek i souborů klienta jako CI na `ubuntu-24.04`, a dva běhy `goreleaser release` s testovacím tagem daly stejný `checksums.txt` včetně archivů. Publikovaný `checksums.txt` zatím neexistuje, protože žádné vydání ještě nebylo; porovnání s ním ověří první vydání.
- Postup vydání a co maintainer zveřejní je v `docs/release-checklist.md`.

Cena: maintainer potřebuje mise a asi minutu sestavení. Chrání to jen proti záměně v sestavení na GitHubu, ne proti změně zdroje, kterou maintainer sám přehlédne; proti té pomáhá povinná revize a ochrana větví, kterou nastavuje vlastník.

## D65: rotace logů v Compose

Oba kontejnery v `deploy/compose` logují přes ovladač `local` s nejvýš třemi soubory po 10 MB. Výchozí ovladač `json-file` logy neomezuje, takže by časem zaplnil disk se zprávami; `NYLZO_MIN_FREE_BYTES` by pak zastavil vytváření. `nylzo` neloguje id ani adresy (spec §10) a Caddy v příkladu neloguje požadavky, takže limit velikosti stačí.
