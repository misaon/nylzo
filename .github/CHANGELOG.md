# Changelog

## [0.14.0](https://github.com/misaon/nylzo/compare/v0.13.0...v0.14.0) (2026-11-14)


### ⚠ BREAKING CHANGES

* **security:** the Compose example needs Nylzo 0.14.0 or later, the tmpfiles entry from nylzo-epoch.conf, and a host without swap or with a noswap tmpfs on /run/nylzo/epoch. A host reboot ends every live message. A deployment that takes the new compose.yaml must first move epoch/seed out of nylzo-data, as the Self-hosting guide shows.
* **deploy:** the Compose example needs Docker Engine 28.0 or newer. Engine 27 refuses the isolated gateway mode; Engine 26 and older ignore it, which check-isolation reports.
* **security:** the fragment and envelope move to version 3 and the first start of serve erases every message of protocol v2. Clients of protocol v2 get client_outdated and must be updated.
* **security:** messages now seal their deadline (envelope flag 2) and the TTL runs from the instance response. A CLI before this release can neither create nor read such messages and gets client_outdated, and the seed file of an unreleased main build (nylzo-e2) is refused as damaged. Ship server and CLI together.
* **security:** clients from before this release send an X25519 share key and get 400 client_outdated; a client of this release gets 400 bad_request from an older server. Ship server and CLI together.

### Features

* **security:** check passphrases with OPAQUE in protocol v3 ([#360](https://github.com/misaon/nylzo/issues/360)) ([8dfe6e6](https://github.com/misaon/nylzo/commit/8dfe6e60f78b28c5aab6133c93235f8d2108fa33))


### Fixes

* **automation:** hold go.step.sm/crypto until Caddy raises it ([#386](https://github.com/misaon/nylzo/issues/386)) ([cecfeb5](https://github.com/misaon/nylzo/commit/cecfeb5ff486e5656c710045df333d4b11071827))
* **automation:** unblock the quic-go, regctl and Ubuntu 26.04 Arm updates ([#385](https://github.com/misaon/nylzo/issues/385)) ([5b3398d](https://github.com/misaon/nylzo/commit/5b3398d7f53c18d530973e68324bba088bdca27a))
* **backend:** update module golang.org/x/crypto to v0.59.0 [security] (main) ([#338](https://github.com/misaon/nylzo/issues/338)) ([3b05d77](https://github.com/misaon/nylzo/commit/3b05d77dffd99082ab10cdeb7b53314f13b51785))
* **backend:** update OpenTelemetry to v1.47.0 in the Caddy build ([#377](https://github.com/misaon/nylzo/issues/377)) ([2ec368c](https://github.com/misaon/nylzo/commit/2ec368c8f93da6db77cd8b73ac533be74931b1d1))
* **cli:** end by the stop signal after cleanup ([#353](https://github.com/misaon/nylzo/issues/353)) ([2f95b98](https://github.com/misaon/nylzo/commit/2f95b98623ef198c7c5cbeb4839b558ef387e5b9))
* **cli:** make the read --out directory after the passphrase prompt ([#358](https://github.com/misaon/nylzo/issues/358)) ([5f50c39](https://github.com/misaon/nylzo/commit/5f50c39c05c45be66b81a4938b5cd30b801d6d2f))
* **deploy:** enforce the encrypted data volume in the strict profile (F20) ([#378](https://github.com/misaon/nylzo/issues/378)) ([8d3e62d](https://github.com/misaon/nylzo/commit/8d3e62ddbfa104172b68dd5e469170207c03bc33))
* **deploy:** isolate the Compose backend from the internet and the host (F19) ([#379](https://github.com/misaon/nylzo/issues/379)) ([26bb995](https://github.com/misaon/nylzo/commit/26bb99525ce75ea6675d69f3617db7c2416acebd))
* **deploy:** keep stored messages open across a pepper rotation on Hetzner ([#348](https://github.com/misaon/nylzo/issues/348)) ([df1934f](https://github.com/misaon/nylzo/commit/df1934f24e48d67d04781acbb0bbab0764906c33))
* end messages a host reboot left unreadable as lost (F21) ([#381](https://github.com/misaon/nylzo/issues/381)) ([67f0dcd](https://github.com/misaon/nylzo/commit/67f0dcd8dc192578879828f910bd718e3faed081))
* **security:** ask before every attachment download ([#341](https://github.com/misaon/nylzo/issues/341)) ([d280a57](https://github.com/misaon/nylzo/commit/d280a57705cc9ec2a3b65d759a3513873ccde258))
* **security:** bind share and lease headers to request signatures ([#330](https://github.com/misaon/nylzo/issues/330)) ([82f5303](https://github.com/misaon/nylzo/commit/82f53037ae27338058534b57a51489fb226cdbdc))
* **security:** check a tag over every stored blob before spending an open ([#364](https://github.com/misaon/nylzo/issues/364)) ([50d63dd](https://github.com/misaon/nylzo/commit/50d63ddd79c77c474c23d08a57863ba00af6bd94))
* **security:** end the expiry share at the message deadline ([#366](https://github.com/misaon/nylzo/issues/366)) ([344b8ff](https://github.com/misaon/nylzo/commit/344b8fff5104eded8a172ccd0f3c8aca8804e535))
* **security:** harden CLI processes before they read secrets ([#337](https://github.com/misaon/nylzo/issues/337)) ([ffa0667](https://github.com/misaon/nylzo/commit/ffa06676ba054e03fb42551c4a73ce82a3e10199))
* **security:** keep passphrase verifiers out of every log ([#357](https://github.com/misaon/nylzo/issues/357)) ([a0aec34](https://github.com/misaon/nylzo/commit/a0aec34ea816a106d99787a0cd768045c8ea533b))
* **security:** keep read running until a claimed message is saved ([#349](https://github.com/misaon/nylzo/issues/349)) ([191213e](https://github.com/misaon/nylzo/commit/191213e2773204f81ded53f06c6c7d3da7231d4a))
* **security:** keep request challenges in memory, out of the database ([#335](https://github.com/misaon/nylzo/issues/335)) ([9b1059f](https://github.com/misaon/nylzo/commit/9b1059f488af10e39cd48e684cc503df7eff086e))
* **security:** keep the Compose epoch seed on host tmpfs (F18) ([#380](https://github.com/misaon/nylzo/issues/380)) ([1414f10](https://github.com/misaon/nylzo/commit/1414f106c7c54b7d43f68c8073c99a473550aa6c))
* **security:** mark files that read writes as downloaded from the internet ([#344](https://github.com/misaon/nylzo/issues/344)) ([a9f34b0](https://github.com/misaon/nylzo/commit/a9f34b0aaa41fb1676bcd26ea714cd56d985d050))
* **security:** rebuild with Go 1.27.4 ([#327](https://github.com/misaon/nylzo/issues/327)) ([d790cbe](https://github.com/misaon/nylzo/commit/d790cbee51609a5ecc37271a0ae83295a883e7ff))
* **security:** renew epoch seeds with fresh entropy every hour ([#354](https://github.com/misaon/nylzo/issues/354)) ([3094e54](https://github.com/misaon/nylzo/commit/3094e5489f5ac5c1b398f031ef1579323c053a6e))
* **security:** require the passphrase to delete a protected message ([#340](https://github.com/misaon/nylzo/issues/340)) ([591dcbb](https://github.com/misaon/nylzo/commit/591dcbb5a9f018be4c6199166f55377388c748ce))
* **security:** seal record key wraps with a key from the pepper ([#347](https://github.com/misaon/nylzo/issues/347)) ([8541282](https://github.com/misaon/nylzo/commit/854128268c694d8036f35530769b77fe83147ba9))
* **security:** sign macOS release binaries with the hardened runtime ([#345](https://github.com/misaon/nylzo/issues/345)) ([34c8477](https://github.com/misaon/nylzo/commit/34c847746d0ee9de7ee97db2abd556a0a9f1b7af))
* **security:** warn after read saves an attachment ([#346](https://github.com/misaon/nylzo/issues/346)) ([35d2473](https://github.com/misaon/nylzo/commit/35d24733f9226775fb0fbaf34797edba4d7d023c))
* **security:** wrap expiry shares with hybrid post-quantum HPKE ([#361](https://github.com/misaon/nylzo/issues/361)) ([4894da7](https://github.com/misaon/nylzo/commit/4894da7ea544c4d9e1e2f588fb4d26c1f52b2539))
* **security:** write private CLI files owner-only on local disks ([#336](https://github.com/misaon/nylzo/issues/336)) ([5f4c20f](https://github.com/misaon/nylzo/commit/5f4c20f22bdf05ad2396cc95a82d68b2e09ef965))
* **web:** keep tracked sent messages in the tab by default (F16) ([#365](https://github.com/misaon/nylzo/issues/365)) ([2d4ce75](https://github.com/misaon/nylzo/commit/2d4ce75021fe839de0fabbd36e0f16f059d196c2))
* **web:** remove third-party analytics from message forms ([#329](https://github.com/misaon/nylzo/issues/329)) ([7161e40](https://github.com/misaon/nylzo/commit/7161e40376568f1a0ad9caac5ea73598386e7d6f))


### Build

* **infrastructure:** update infrastructure dependencies (main) ([#350](https://github.com/misaon/nylzo/issues/350)) ([4ccfaac](https://github.com/misaon/nylzo/commit/4ccfaac207905439bfc9b841a4dbcc74a703d47a))
* **infrastructure:** update infrastructure dependencies to v2.20.1 (main) ([#324](https://github.com/misaon/nylzo/issues/324)) ([1e64ec5](https://github.com/misaon/nylzo/commit/1e64ec5237d3ab8df9d125f3fcfbd868ec8068e5))


### CI

* **deps:** replace actions/attest-build-provenance action with actions/attest v4.2.2 (main) ([#279](https://github.com/misaon/nylzo/issues/279)) ([52beb8a](https://github.com/misaon/nylzo/commit/52beb8a1cef9a95d7ff58d960c6174e964c00997))
* **deps:** update ci dependencies (main) ([#317](https://github.com/misaon/nylzo/issues/317)) ([ae9db4b](https://github.com/misaon/nylzo/commit/ae9db4b6287943284555149036d275579ce7ea2d))
* **deps:** update ci dependencies (main) ([#351](https://github.com/misaon/nylzo/issues/351)) ([eaab2ce](https://github.com/misaon/nylzo/commit/eaab2ce4fcde0e707abcdd76446611f4efa60d0c))
* **deps:** update ci dependencies (main) ([#356](https://github.com/misaon/nylzo/issues/356)) ([8beb6a0](https://github.com/misaon/nylzo/commit/8beb6a039dab02f284bd6c4469c4c6497de52252))
* **deps:** update ci dependencies (main) ([#370](https://github.com/misaon/nylzo/issues/370)) ([2ec41d0](https://github.com/misaon/nylzo/commit/2ec41d0ba8f8b625f33878a4bce45608963d3a62))
* **deps:** update dependency ubuntu to v26 (main) ([#319](https://github.com/misaon/nylzo/issues/319)) ([fbdcd3e](https://github.com/misaon/nylzo/commit/fbdcd3ecf90ec2b94a8b5a894d6bd31da96bd24d))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.226.1 (main) ([#315](https://github.com/misaon/nylzo/issues/315)) ([111180e](https://github.com/misaon/nylzo/commit/111180e9335c272a3a15cdf2b647415dfdb0b1ae))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.230.0 (main) ([#332](https://github.com/misaon/nylzo/issues/332)) ([82684ea](https://github.com/misaon/nylzo/commit/82684ea06b12e78abfddf59e464e51ad38969923))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.230.1 (main) ([#333](https://github.com/misaon/nylzo/issues/333)) ([56f7944](https://github.com/misaon/nylzo/commit/56f79445a0b68d8ff0f08f28ea3d3e275a188656))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.234.0 (main) ([#334](https://github.com/misaon/nylzo/issues/334)) ([ec0270f](https://github.com/misaon/nylzo/commit/ec0270f93a1ee9da28a42f2a5e39be6a190631b3))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.234.1 (main) ([#342](https://github.com/misaon/nylzo/issues/342)) ([318af6c](https://github.com/misaon/nylzo/commit/318af6c3d5dae14820c9eec14e9d73c3c726fded))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.235.2 (main) ([#352](https://github.com/misaon/nylzo/issues/352)) ([7ac1ce5](https://github.com/misaon/nylzo/commit/7ac1ce5404f67175be0940c943389ed65df34825))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.235.3 (main) ([#355](https://github.com/misaon/nylzo/issues/355)) ([ed7d52f](https://github.com/misaon/nylzo/commit/ed7d52ffeeb6cfcf29190fdbf6a9a8d19b86b818))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.236.2 (main) ([#359](https://github.com/misaon/nylzo/issues/359)) ([e304688](https://github.com/misaon/nylzo/commit/e30468883bbc8fcec88e98422857cdd6e3d818bb))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.236.5 (main) ([#362](https://github.com/misaon/nylzo/issues/362)) ([f3f505c](https://github.com/misaon/nylzo/commit/f3f505c8cd2bd664a4c38cbe7e297913a4c24f45))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.237.3 (main) ([#367](https://github.com/misaon/nylzo/issues/367)) ([8543735](https://github.com/misaon/nylzo/commit/8543735ff3cf18f977a584318fc8da33a3ca2a4a))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.238.1 (main) ([#368](https://github.com/misaon/nylzo/issues/368)) ([2651942](https://github.com/misaon/nylzo/commit/2651942978f743f353daedfd91c643e2dd94970e))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.238.2 (main) ([#369](https://github.com/misaon/nylzo/issues/369)) ([0748769](https://github.com/misaon/nylzo/commit/0748769c92f1738b88c7fef09ebbd246774f943e))
* **security:** isolate native release builds from publication ([#331](https://github.com/misaon/nylzo/issues/331)) ([26dfa48](https://github.com/misaon/nylzo/commit/26dfa482d29ecbe09f22bf5ed1f6069bda2d7ccd))


### Tests

* **guard:** derive the newer override patch from the locked version ([#383](https://github.com/misaon/nylzo/issues/383)) ([366c08c](https://github.com/misaon/nylzo/commit/366c08c8a248fc85ffddf7344a50df533375c666))


### Maintenance

* **config:** migrate Renovate config ([#343](https://github.com/misaon/nylzo/issues/343)) ([e8f1d19](https://github.com/misaon/nylzo/commit/e8f1d193b1dcaeeb7e03f59ab4e4a382d7dee73e))

## [0.13.0](https://github.com/misaon/nylzo/compare/v0.12.0...v0.13.0) (2026-11-10)


### Features

* **web:** count home page visits on nylzo.com with Cloudflare Web Analytics ([#313](https://github.com/misaon/nylzo/issues/313)) ([2dc3820](https://github.com/misaon/nylzo/commit/2dc3820c98fdbe66fdfaef8d285810071a70ec99))
* **web:** lead with verified strengths and keep limits on Transparency ([#311](https://github.com/misaon/nylzo/issues/311)) ([34acb45](https://github.com/misaon/nylzo/commit/34acb459b1b9e5796507b33fb146cb0525f10cc2))
* **web:** redesign the home page sections and show the real tool logos ([#314](https://github.com/misaon/nylzo/issues/314)) ([d2f7ac1](https://github.com/misaon/nylzo/commit/d2f7ac131e3313bf2cd27d77892ae3d79ddc9278))


### Fixes

* **automation:** one Renovate group per area and accept a dropped toolchain line ([#303](https://github.com/misaon/nylzo/issues/303)) ([db8728e](https://github.com/misaon/nylzo/commit/db8728e0aa3d1b4a8a08e73a5a2651cf31b797b8))


### Refactoring

* **web:** simplify the Svelte client ([#309](https://github.com/misaon/nylzo/issues/309)) ([a70c593](https://github.com/misaon/nylzo/commit/a70c5936e37a5c63c5c3489b6347c6588d4630d6))


### CI

* **deps:** update ci dependencies (main) ([#306](https://github.com/misaon/nylzo/issues/306)) ([35467aa](https://github.com/misaon/nylzo/commit/35467aa7f48943e7edf563874c0308e319d5b997))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.224.2 (main) ([#310](https://github.com/misaon/nylzo/issues/310)) ([14cfd7c](https://github.com/misaon/nylzo/commit/14cfd7c4117acdae643927c25a134d5d935e73be))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.226.0 (main) ([#312](https://github.com/misaon/nylzo/issues/312)) ([a3c8ff7](https://github.com/misaon/nylzo/commit/a3c8ff724ae6953ca1afb10da410629d6183d5d6))

## [0.12.0](https://github.com/misaon/nylzo/compare/v0.11.0...v0.12.0) (2026-11-09)


### Features

* act on the findings of the external site tests ([#300](https://github.com/misaon/nylzo/issues/300)) ([d14b9bd](https://github.com/misaon/nylzo/commit/d14b9bd0720a69475621c1092a7c71b9448697c1))
* install the CLI with Homebrew and Scoop ([#301](https://github.com/misaon/nylzo/issues/301)) ([2638c94](https://github.com/misaon/nylzo/commit/2638c94425ea95fbf9308f73131cede6860ae621))
* **web:** add ten languages, a view countdown and UI fixes ([#302](https://github.com/misaon/nylzo/issues/302)) ([a6be5b0](https://github.com/misaon/nylzo/commit/a6be5b03fc8cdab500cc55bee77b330b525bab5d))


### Fixes

* **dependency-guard:** accept version bumps of existing overrides ([#297](https://github.com/misaon/nylzo/issues/297)) ([ff9b776](https://github.com/misaon/nylzo/commit/ff9b7764bd254917d5435a8308da8ee3176ada82))


### Build

* **infrastructure:** update dependency osv-scanner to v2.7.1 (main) ([#298](https://github.com/misaon/nylzo/issues/298)) ([1c35c61](https://github.com/misaon/nylzo/commit/1c35c61de19ed4a1723a3b04797d99b71037efb1))


### CI

* **deps:** update ci dependencies (main) ([#280](https://github.com/misaon/nylzo/issues/280)) ([059f94b](https://github.com/misaon/nylzo/commit/059f94b1e334551fa3cddd26cdea8bfb8d2c034d))


### Maintenance

* **web:** remove the Seznam webmaster verification tag ([#295](https://github.com/misaon/nylzo/issues/295)) ([3a8de44](https://github.com/misaon/nylzo/commit/3a8de44807baf1a2623288a99e67ba5e2aaebb3a))

## [0.11.0](https://github.com/misaon/nylzo/compare/v0.10.0...v0.11.0) (2026-11-09)


### Features

* **web:** add the Seznam webmaster verification tag ([#293](https://github.com/misaon/nylzo/issues/293)) ([727bba4](https://github.com/misaon/nylzo/commit/727bba4fa58e107595e1686d8449615e1f666fc2))

## [0.10.0](https://github.com/misaon/nylzo/compare/v0.9.0...v0.10.0) (2026-11-09)


### Fixes

* **deploy:** let search engine crawlers load public pages while messages stay post-quantum ([#289](https://github.com/misaon/nylzo/issues/289)) ([637f487](https://github.com/misaon/nylzo/commit/637f487629f51be1fbe9d8fa50731c7f39ac7db1))
* **deploy:** start the IPv6 unit on running hosts and quiet the docker CLI ([#266](https://github.com/misaon/nylzo/issues/266)) ([c668766](https://github.com/misaon/nylzo/commit/c66876626b108aa5a3378744bae5200cc7e25948))
* **deps:** let the guard pass mise lock variants that repeat a verified asset ([#268](https://github.com/misaon/nylzo/issues/268)) ([0fd1f65](https://github.com/misaon/nylzo/commit/0fd1f65083c2b8a80bf24d143bce018cfe1546b5))
* **release:** leave the release commit out of its own release notes ([#274](https://github.com/misaon/nylzo/issues/274)) ([8fc20e6](https://github.com/misaon/nylzo/commit/8fc20e65e3ed51a5ba8800ed4dafcdfd2c77a804))
* **release:** scan the release archives so Grype matches Go code by symbol ([#270](https://github.com/misaon/nylzo/issues/270)) ([3efcd8f](https://github.com/misaon/nylzo/commit/3efcd8f51e59aad1744f5649383a9f634a1998ec))


### Documentation

* correct the README and Wiki facts and publish every Wiki page ([#271](https://github.com/misaon/nylzo/issues/271)) ([7dd84a0](https://github.com/misaon/nylzo/commit/7dd84a027b5c8e248b9ecba3a05119eb3e595207))


### Refactoring

* **deploy:** let OpenTofu enforce its pinned version and drop the duplicate pins ([#278](https://github.com/misaon/nylzo/issues/278)) ([2d40df3](https://github.com/misaon/nylzo/commit/2d40df3d57ca682b2e07a9315525088e8a9ca6b9))
* remove dead code and configuration the audit found ([#272](https://github.com/misaon/nylzo/issues/272)) ([cfc2092](https://github.com/misaon/nylzo/commit/cfc20924d0a722de591e3de557485d592279bbdc))
* simplify repository tools and configs, fix two fail-open checks ([#290](https://github.com/misaon/nylzo/issues/290)) ([1207acf](https://github.com/misaon/nylzo/commit/1207acf51a1529e6e2a161ebf8459b33de62a5ef))


### CI

* let Scorecard see the vulnerabilities the SCA policy already assessed ([#273](https://github.com/misaon/nylzo/issues/273)) ([b12be93](https://github.com/misaon/nylzo/commit/b12be93cc040a19369442835243c6a6dc2e119fa))
* publish Scorecard results and dispatch workflows with API 2026-03-10 ([#269](https://github.com/misaon/nylzo/issues/269)) ([1115195](https://github.com/misaon/nylzo/commit/111519550557bbef7742f882076c890d2630bb71))
* **release:** attach build provenance as an in-toto release asset ([#275](https://github.com/misaon/nylzo/issues/275)) ([963c30e](https://github.com/misaon/nylzo/commit/963c30e54a74479d2e708facd39dca54c5ec8e9f))
* simplify the workflows without changing what they run ([#276](https://github.com/misaon/nylzo/issues/276)) ([952f4bc](https://github.com/misaon/nylzo/commit/952f4bc79d5c5f31d48f3cc0e04238dc167a1c26))


### Tests

* **store:** let raw test connections wait for the background scrub ([#277](https://github.com/misaon/nylzo/issues/277)) ([28dee8a](https://github.com/misaon/nylzo/commit/28dee8a0962693eda5f4b659cf7ca26fc8b2f210))

## [0.9.0](https://github.com/misaon/nylzo/compare/v0.8.0...v0.9.0) (2026-11-08)


### Fixes

* **ci:** pass the environment secrets to the infrastructure stack workflow ([#263](https://github.com/misaon/nylzo/issues/263)) ([c7b8cd8](https://github.com/misaon/nylzo/commit/c7b8cd8209c943118ff0ca294cfcd4b2fcb53349))
* **deploy:** configure IPv6, prune old images and quiet the host logs ([#256](https://github.com/misaon/nylzo/issues/256)) ([9bda0e2](https://github.com/misaon/nylzo/commit/9bda0e299ef59bc67ed98641397fb3b00c631962))
* **deps:** give Renovate its documented App permissions and fail aborted runs ([#255](https://github.com/misaon/nylzo/issues/255)) ([e722cb1](https://github.com/misaon/nylzo/commit/e722cb122f2444d6c733766ce44dd90dc8d2909d))


### Refactoring

* **server:** review the Go server for security, flooding and structure ([#260](https://github.com/misaon/nylzo/issues/260)) ([ae401b3](https://github.com/misaon/nylzo/commit/ae401b3c0d490dbe92719b25448d23c279780288))


### CI

* drop the acceptance criterion ids from job names ([#261](https://github.com/misaon/nylzo/issues/261)) ([1ff855e](https://github.com/misaon/nylzo/commit/1ff855ea8d681a5d588bf04e7e877748262982c9))
* **infrastructure:** keep the state endpoint secret and store state in R2 ([#254](https://github.com/misaon/nylzo/issues/254)) ([ec38869](https://github.com/misaon/nylzo/commit/ec3886937aa87929a02efa73ba9432487a14003a))


### Tests

* **e2e:** measure sent list positions after the entrance animation ([#262](https://github.com/misaon/nylzo/issues/262)) ([d32237a](https://github.com/misaon/nylzo/commit/d32237a919549b86843694abe27220bb4c429a05))

## [0.8.0](https://github.com/misaon/nylzo/compare/v0.7.0...v0.8.0) (2026-11-07)


### ⚠ BREAKING CHANGES

* **web:** the web client no longer has the /report page and the server answers GET /report with 404. Abuse goes to the operator contact on /transparency.

### Features

* **web:** implement the redesigned web client ([#245](https://github.com/misaon/nylzo/issues/245)) ([078926d](https://github.com/misaon/nylzo/commit/078926d4068afa6fced79f019f3e418eb90501f0))


### Fixes

* **security:** harden the web client, its pages and their deployment ([#249](https://github.com/misaon/nylzo/issues/249)) ([9d26900](https://github.com/misaon/nylzo/commit/9d2690056a199dfac9cf67bfbc32af63ce2f19c5))
* **web:** show focus, honour reduced motion and drop Geist Mono ([#247](https://github.com/misaon/nylzo/issues/247)) ([0fa92a4](https://github.com/misaon/nylzo/commit/0fa92a410cceaae013983d2c2c9c1395f1b05a5c))


### Refactoring

* **web:** review the Svelte client with no visual change ([#248](https://github.com/misaon/nylzo/issues/248)) ([9ca0552](https://github.com/misaon/nylzo/commit/9ca0552219ed79799ebdc4356a385d6e89735e39))
* **web:** tidy the CSS and Tailwind classes without changing the output ([#246](https://github.com/misaon/nylzo/issues/246)) ([a2be466](https://github.com/misaon/nylzo/commit/a2be4667b52aa83d5b05e5a1c29203d92862a001))


### CI

* automate delivery, watch production and deploy Hetzner from Actions ([#250](https://github.com/misaon/nylzo/issues/250)) ([3b8b3ad](https://github.com/misaon/nylzo/commit/3b8b3adb407a0643a696d998a43a8960c782df96))


### Maintenance

* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.221.4 (main) ([#237](https://github.com/misaon/nylzo/issues/237)) ([e59d137](https://github.com/misaon/nylzo/commit/e59d1375b9921f3067b0c5f175a677a711bf41ee))
* **deps:** update module github.com/klauspost/compress to v1.20.2 [security] (main) ([#241](https://github.com/misaon/nylzo/issues/241)) ([56acf32](https://github.com/misaon/nylzo/commit/56acf32c7c806c3e72bd0be0df850c82ebab4fe2))

## [0.7.0](https://github.com/misaon/nylzo/compare/v0.6.0...v0.7.0) (2026-11-04)


### Fixes

* **release:** paginate source verification checks ([#239](https://github.com/misaon/nylzo/issues/239)) ([0567089](https://github.com/misaon/nylzo/commit/0567089f20cdb7d9dbb8e688e7375e3ae1281edf))

## [0.6.0](https://github.com/misaon/nylzo/compare/v0.5.0...v0.6.0) (2026-11-03)


### Fixes

* **deploy:** stop live timers before masking their units ([#233](https://github.com/misaon/nylzo/issues/233)) ([5fd99bb](https://github.com/misaon/nylzo/commit/5fd99bbf9dc9e16308f16867e9e3f7d2cf7b50a0))
* **security:** bound domain monitoring and runtime downloads ([#236](https://github.com/misaon/nylzo/issues/236)) ([ba743e9](https://github.com/misaon/nylzo/commit/ba743e987e27160ef648f311cf4f68fcd11dafa8))


### Security

* **deploy:** isolate frontend delivery from the API process ([#238](https://github.com/misaon/nylzo/issues/238)) ([3de2bf5](https://github.com/misaon/nylzo/commit/3de2bf5ca798d5acf32dc5421971ae68e062df2b))


### Maintenance

* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.206.2 (main) ([#228](https://github.com/misaon/nylzo/issues/228)) ([23ce86b](https://github.com/misaon/nylzo/commit/23ce86bbbc5e3d736374179a5dbafa1638dbabfb))

## [0.5.0](https://github.com/misaon/nylzo/compare/v0.4.0...v0.5.0) (2026-11-02)


### Fixes

* **deploy:** validate host Caddyfiles and the served certificate ([#230](https://github.com/misaon/nylzo/issues/230)) ([5285a71](https://github.com/misaon/nylzo/commit/5285a7191e0fef22abe1c8b1a86c378253f89e7e))

## [0.4.0](https://github.com/misaon/nylzo/compare/v0.3.0...v0.4.0) (2026-11-02)


### Fixes

* **deploy:** verify host readiness and pin patched container runtimes ([#229](https://github.com/misaon/nylzo/issues/229)) ([a5d6d55](https://github.com/misaon/nylzo/commit/a5d6d550773e24d005155082d8530a723c22d10c))


### Build

* migrate project tooling to Bun and Oxc ([#227](https://github.com/misaon/nylzo/issues/227)) ([66364fa](https://github.com/misaon/nylzo/commit/66364fa326d74fbbef3fd6cdc98080560895b65c))


### Tests

* expand browser, storage and Go command coverage ([#213](https://github.com/misaon/nylzo/issues/213)) ([2c05f9d](https://github.com/misaon/nylzo/commit/2c05f9ddb8a7e9151e073216ba4ba8cbd096b2a9))
* **web:** increase composer and read page coverage ([#211](https://github.com/misaon/nylzo/issues/211)) ([699624d](https://github.com/misaon/nylzo/commit/699624dcdcfe7e5e801c531cc8e81878816b1c97))


### Maintenance

* **deps:** update dependency codecov/codecov-cli to v11.3.2 (main) ([#215](https://github.com/misaon/nylzo/issues/215)) ([4f458ec](https://github.com/misaon/nylzo/commit/4f458ec9ad490917017149563a5fb7d05a04c2bd))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.199.0 (main) ([#210](https://github.com/misaon/nylzo/issues/210)) ([85d459e](https://github.com/misaon/nylzo/commit/85d459eb7924d52bcf49c2484dbd3dfe9a8c9dde))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.199.2 (main) ([#214](https://github.com/misaon/nylzo/issues/214)) ([767e735](https://github.com/misaon/nylzo/commit/767e7354f5fa4225cf8bdc008624057ed116c4c6))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.200.0 (main) ([#226](https://github.com/misaon/nylzo/issues/226)) ([135fda9](https://github.com/misaon/nylzo/commit/135fda9c96e311ae22deb54856ea6df89485f5df))
* **deps:** update pnpm to v12.16.1 (main) ([#219](https://github.com/misaon/nylzo/issues/219)) ([0559183](https://github.com/misaon/nylzo/commit/055918361f288105abd3b6c08c2c4748933c76dd))
* **deps:** update step-security/harden-runner action to v2.22.3 (main) ([#217](https://github.com/misaon/nylzo/issues/217)) ([dd745c0](https://github.com/misaon/nylzo/commit/dd745c0a67699d5ad46e4a5ae0b9da5af057aeab))

## [0.3.0](https://github.com/misaon/nylzo/compare/v0.2.0...v0.3.0) (2026-11-01)

### Upgrade notes

This release follows a full security audit. Read these before you upgrade an instance.

* The first start erases every message stored by 0.1 or 0.2, including unread ones. Their links then show a notice that the message was deleted. Tell your users before you upgrade.
* The `nylzo` CLI 0.1 and 0.2 can no longer create or read messages. Update it.
* The server runs only on Linux, with the data directory on ext4 (without journalled data) or XFS. It needs unlimited locked memory (`--ulimit memlock=-1:-1`, or `memlock: -1` in Compose) and reads the pepper only from `NYLZO_PEPPER_FILE`. `NYLZO_PEPPER` in the environment is refused.
* The expiry key seed lives in `NYLZO_EPOCH_DIR`, by default `epoch` inside the data directory. On a tmpfs, no disk image or backup holds it, but a host reboot ends every live message. The Hetzner Flatcar profile uses a tmpfs.
* Both deployment profiles accept only TLS 1.3 with X25519MLKEM768, and SSH accepts only post-quantum key exchange. Safari and iOS before 26, Firefox ESR 128, clients on OpenSSL before 3.5, and SSH clients without `mlkem768x25519-sha256` or `sntrup761x25519-sha512` cannot connect.
* Compose needs `NYLZO_CADDY_IMAGE` and runs Caddy as UID 65533, with its state in the `caddy-state` directory.
* A Hetzner Flatcar host keeps 0.2 until you run the one-time host step in the Hetzner Flatcar guide. Run it after this release is published.


### ⚠ BREAKING CHANGES

* **protocol:** version 2 links, committed envelopes and expiring key shares ([#206](https://github.com/misaon/nylzo/issues/206))
* **deploy:** both deployment profiles accept only TLS 1.3 with X25519MLKEM768. Safari and iOS before 26, Firefox ESR 128, clients on OpenSSL before 3.5, and many link-preview bots and uptime monitors cannot connect.
* **cli:** serve each local client session from its own origin ([#207](https://github.com/misaon/nylzo/issues/207))
* **security:** build Caddy from source and ship it in each release ([#181](https://github.com/misaon/nylzo/issues/181))

### Features

* **cli:** verified local web client with nylzo web and nylzo open ([#201](https://github.com/misaon/nylzo/issues/201)) ([95618c8](https://github.com/misaon/nylzo/commit/95618c80d3034a0790799485cc27a2370e4351d8))
* **protocol:** version 2 links, committed envelopes and expiring key shares ([#206](https://github.com/misaon/nylzo/issues/206)) ([f91b125](https://github.com/misaon/nylzo/commit/f91b1251f935b93aa6684d6a9be60429c273c031))


### Fixes

* **ci:** request only the App permissions Renovate is granted ([#184](https://github.com/misaon/nylzo/issues/184)) ([a744685](https://github.com/misaon/nylzo/commit/a744685eed5bbd13a433ac86886ea7ba9f62a32b))
* **cli:** serve each local client session from its own origin ([#207](https://github.com/misaon/nylzo/issues/207)) ([446b865](https://github.com/misaon/nylzo/commit/446b8654608bb5fc2ca987401acad91daa4037c5))
* **deploy:** accept older SSH clients and name who the TLS policy shuts out ([#208](https://github.com/misaon/nylzo/issues/208)) ([6dd9990](https://github.com/misaon/nylzo/commit/6dd9990576c44d34272739b8546cb393cd6bc370))
* **deploy:** harden hosts, TLS and the update path ([#202](https://github.com/misaon/nylzo/issues/202)) ([792fa1b](https://github.com/misaon/nylzo/commit/792fa1b17ab0fa03b1684a3d76884922d6a56c56))
* **erase:** refuse ext4 journalled data from superblock defaults and close the server Low items ([#209](https://github.com/misaon/nylzo/issues/209)) ([4cad6df](https://github.com/misaon/nylzo/commit/4cad6df3e3ced85eb57d5b7ca2a0bb19cf6996f8))
* **security:** build Caddy from source and ship it in each release ([#181](https://github.com/misaon/nylzo/issues/181)) ([66789a0](https://github.com/misaon/nylzo/commit/66789a0c1de979a554dd9ff9533b232575b70648))
* **server:** erase deleted messages on disk and keep secrets out of memory and logs ([#204](https://github.com/misaon/nylzo/issues/204)) ([a068cd9](https://github.com/misaon/nylzo/commit/a068cd90a43dcf4e0101bb913c11c7d93147a933))
* **web:** correct the Czech copy of the link and verified-client texts ([#205](https://github.com/misaon/nylzo/issues/205)) ([7d75643](https://github.com/misaon/nylzo/commit/7d75643a37151357cdebd177c168de7ded8059ba))
* **web:** keep link secrets off disk and out of browser history ([#203](https://github.com/misaon/nylzo/issues/203)) ([9ad33f6](https://github.com/misaon/nylzo/commit/9ad33f65da6659e9bc5f6629449dcd3b2d5c9cb5))


### Refactoring

* **automation:** move the SCA gate and web install into the automation workspace ([#185](https://github.com/misaon/nylzo/issues/185)) ([bae0503](https://github.com/misaon/nylzo/commit/bae05033752d0dc9660f19e4148326dae36c65de))
* **go:** remove dead code and pre-release migrations ([#196](https://github.com/misaon/nylzo/issues/196)) ([e211e61](https://github.com/misaon/nylzo/commit/e211e61947e881c4c04f43a163fd03269473c795))
* **go:** split oversized packages and move server tests to synctest ([#200](https://github.com/misaon/nylzo/issues/200)) ([de966d3](https://github.com/misaon/nylzo/commit/de966d33abe18ce7bd911903af77e7883f1a190e))
* remove dead TypeScript exports, obsolete browser fallbacks and duplicate parsers ([#197](https://github.com/misaon/nylzo/issues/197)) ([80f4abe](https://github.com/misaon/nylzo/commit/80f4abe4ed77917896380d80ed74f791da88e1ae))
* **web:** style the sent, report and transparency screens with utilities ([#199](https://github.com/misaon/nylzo/issues/199)) ([594d435](https://github.com/misaon/nylzo/commit/594d435d633670e37c08df80f4b08f6b9a0dcc7c))


### Build

* move the toolchain to Node.js 26 LTS ([#182](https://github.com/misaon/nylzo/issues/182)) ([8e616e8](https://github.com/misaon/nylzo/commit/8e616e8d405e50a17f6d5efe1f458f9b60448d47))


### CI

* pin Go from mise.lock and cut queueing in the pipeline ([#198](https://github.com/misaon/nylzo/issues/198)) ([49df0d5](https://github.com/misaon/nylzo/commit/49df0d56d5bd60a6d3f152c2f4c4d19588eb5c5d))
* run the Flatcar acceptance job on Ubuntu 26.04 ([#183](https://github.com/misaon/nylzo/issues/183)) ([5315b24](https://github.com/misaon/nylzo/commit/5315b24a50b7d6ec03af096468284983f04dc254))


### Maintenance

* **deps:** update caddy:2.11.7 docker digest to 36c24ad (main) ([#186](https://github.com/misaon/nylzo/issues/186)) ([dab0f2f](https://github.com/misaon/nylzo/commit/dab0f2fc73f6c4f5019abcb487056ddb69389928))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.198.3 (main) ([#188](https://github.com/misaon/nylzo/issues/188)) ([4025fd1](https://github.com/misaon/nylzo/commit/4025fd127a6b02727d4b8c3d4ea7ee1da301705d))
* **deps:** update goreleaser to v2.19.0 (main) ([#189](https://github.com/misaon/nylzo/issues/189)) ([3f29aa6](https://github.com/misaon/nylzo/commit/3f29aa6a339fc77b2ce04d816766e26136a61f50))
* drop unused configuration and document repository tooling ([#195](https://github.com/misaon/nylzo/issues/195)) ([e4064a5](https://github.com/misaon/nylzo/commit/e4064a55a8d467b46d5bac7cfd030d9f7d84e5c4))

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
