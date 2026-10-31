# Changelog

## [0.2.0](https://github.com/misaon/nylzo/compare/v0.1.8...v0.2.0) (2026-10-31)


### ⚠ BREAKING CHANGES

* **automation:** replace the dependency coordinator with native Renovate ([#179](https://github.com/misaon/nylzo/issues/179))

### Features

* **core:** compress large attachments before encryption ([#166](https://github.com/misaon/nylzo/issues/166)) ([f9a35ec](https://github.com/misaon/nylzo/commit/f9a35ec90a50c068393dbf1311f7ecaf2b4137a7))
* **web:** declare supported browsers with Browserslist ([#180](https://github.com/misaon/nylzo/issues/180)) ([d4987cf](https://github.com/misaon/nylzo/commit/d4987cfe94bcf0e1c64c0d68622b2ffdb4524e96))


### Fixes

* **ci:** authorize ancestor tags and retry transparency verification ([#76](https://github.com/misaon/nylzo/issues/76)) ([dfb1713](https://github.com/misaon/nylzo/commit/dfb1713aa5a96aabb3c7f45fa5e64b0fed1439da))
* **ci:** continue updates after manual migration proposals ([#75](https://github.com/misaon/nylzo/issues/75)) ([2387e75](https://github.com/misaon/nylzo/commit/2387e75848aac2b6e622db0e75e74cd2520eddb1))
* **ci:** distinguish job timeouts from manual cancellations ([#114](https://github.com/misaon/nylzo/issues/114)) ([1ef1ac1](https://github.com/misaon/nylzo/commit/1ef1ac1b5d504c5844cc5142472a1caec96ad53a))
* **ci:** isolate and pin native mise lock generation ([#112](https://github.com/misaon/nylzo/issues/112)) ([d01ecef](https://github.com/misaon/nylzo/commit/d01eceff6df2b94cc2a0fb057d9c1f13012f01d2))
* **ci:** provide verified Renovate tooling and deduplicate CLI inputs ([#90](https://github.com/misaon/nylzo/issues/90)) ([24afe31](https://github.com/misaon/nylzo/commit/24afe314b3234a111a4715bac57e51754d57e864))
* **ci:** recover transient maintenance failures and detect stalled publication ([#113](https://github.com/misaon/nylzo/issues/113)) ([d950b5e](https://github.com/misaon/nylzo/commit/d950b5e9d081e8539e18418cb40081cc7f593262))
* **ci:** restore Renovate cache ownership after cache hits ([#77](https://github.com/misaon/nylzo/issues/77)) ([ef223ef](https://github.com/misaon/nylzo/commit/ef223ef080cd9d75347a27e55d60740ffc8a5278))
* **ci:** validate dependency artifacts and lockfile sources ([#95](https://github.com/misaon/nylzo/issues/95)) ([711878a](https://github.com/misaon/nylzo/commit/711878a1f7cbf30359c00445af21108b463589e3))
* **deps:** deduplicate repeated pins before applying batch limits ([#134](https://github.com/misaon/nylzo/issues/134)) ([61b248f](https://github.com/misaon/nylzo/commit/61b248f4b08dd6f873e7db6f1f4bc46c045468e0))
* **deps:** keep normalized tool snapshots atomic ([#168](https://github.com/misaon/nylzo/issues/168)) ([77fb2f6](https://github.com/misaon/nylzo/commit/77fb2f6add640388b7c2f88af61d7f418395e9f6))
* **deps:** keep update queues bounded and recover verified CI fixes ([#126](https://github.com/misaon/nylzo/issues/126)) ([14ba8d4](https://github.com/misaon/nylzo/commit/14ba8d46b9821a2f596213b128b606dd714caa8f))
* **deps:** publish compatible updates as one release batch ([#129](https://github.com/misaon/nylzo/issues/129)) ([c86be74](https://github.com/misaon/nylzo/commit/c86be74819bd3c74b67ec76b37f4caf9c4b480cd))
* **deps:** recover grouped updates and retire stale diagnostics ([#167](https://github.com/misaon/nylzo/issues/167)) ([d5501b2](https://github.com/misaon/nylzo/commit/d5501b294290887d217f2ec4637dcd79e8c533f5))
* **deps:** recover live batches with a compatible bootstrap and bounded CI ([#137](https://github.com/misaon/nylzo/issues/137)) ([c3531f8](https://github.com/misaon/nylzo/commit/c3531f88e69fe8a31365a34a76829f19b1f5847b))
* **deps:** select complete tool updates and resume diagnostic retries ([#169](https://github.com/misaon/nylzo/issues/169)) ([ff0415e](https://github.com/misaon/nylzo/commit/ff0415ee35bb86a3af7f9631c6cf213d5ae20798))
* **deps:** update docker/login-action action to v4.7.0 (main) ([e6401f4](https://github.com/misaon/nylzo/commit/e6401f4a30f6ab2c21f197a776294e79073b9ebf))
* **deps:** update regroup-146-c112c2c9b60d ([6c0ebae](https://github.com/misaon/nylzo/commit/6c0ebae944188d923fcd7cc58495ed58675b7d48))
* **deps:** update weekly dependency batch (main) ([9a1e80e](https://github.com/misaon/nylzo/commit/9a1e80e1d683ad688c4d7e1ac0b88b880338537d))
* **deps:** update weekly dependency batch (main) ([ef2c01d](https://github.com/misaon/nylzo/commit/ef2c01d356749bd89b4fd613af5b32af8b2c5109))
* **deps:** use reviewed Mise pins for published-source discovery ([#115](https://github.com/misaon/nylzo/issues/115)) ([9616308](https://github.com/misaon/nylzo/commit/9616308d7dff4b03ffa3391173abebf81d30ea86))
* **security:** enforce browser origin and isolation boundaries ([#178](https://github.com/misaon/nylzo/issues/178)) ([b8d8d9a](https://github.com/misaon/nylzo/commit/b8d8d9adb44aa762dd1c5e9aa7545e55feb9788b))
* **web:** enforce resource integrity and dependency security gates ([#170](https://github.com/misaon/nylzo/issues/170)) ([a3e5b68](https://github.com/misaon/nylzo/commit/a3e5b688038173c4a7ead3e3df550dbac4b09182))
* **web:** keep browser keys native and recovery secrets encrypted ([#175](https://github.com/misaon/nylzo/issues/175)) ([2fae03d](https://github.com/misaon/nylzo/commit/2fae03de30f0cf710f854b1bbdfe99cd715e15bd))


### Refactoring

* **automation:** replace the dependency coordinator with native Renovate ([#179](https://github.com/misaon/nylzo/issues/179)) ([b5033bd](https://github.com/misaon/nylzo/commit/b5033bd8124a75be8f62d285661f9d44f8a5a8ba))

## [0.1.8](https://github.com/misaon/nylzo/compare/v0.1.7...v0.1.8) (2026-10-29)


### Fixes

* **ci:** complete Renovate branch setup and retire duplicate updates ([#72](https://github.com/misaon/nylzo/issues/72)) ([0ff4a2f](https://github.com/misaon/nylzo/commit/0ff4a2fd53792ce6f9f5bc89abcd64e83f905ec5))
* **ci:** publish the approved source when main advances ([#71](https://github.com/misaon/nylzo/issues/71)) ([934517d](https://github.com/misaon/nylzo/commit/934517dd7f3cd0bde341ba1df3a3f69a8315c625))
* **security:** scan pinned providers and auxiliary binaries ([#74](https://github.com/misaon/nylzo/issues/74)) ([6dbe411](https://github.com/misaon/nylzo/commit/6dbe411379d81f0ca72255bfbc9b71ae8ef6c4b7))

## [0.1.7](https://github.com/misaon/nylzo/compare/v0.1.6...v0.1.7) (2026-10-29)


### Fixes

* **ci:** retain release events and serialize publication ([#70](https://github.com/misaon/nylzo/issues/70)) ([20bbf84](https://github.com/misaon/nylzo/commit/20bbf84a3a89ce9dff3690fb88660640bdd804bb))


### CI

* isolate planned releases and automate verified dependency patches ([#68](https://github.com/misaon/nylzo/issues/68)) ([1c4273a](https://github.com/misaon/nylzo/commit/1c4273a60a3e16b939c3713d6261b2ef295bfec6))

## Changelog

Release notes through v0.1.6 are available in [GitHub Releases](https://github.com/misaon/nylzo/releases).
