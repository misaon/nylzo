# Changelog

## [0.15.0](https://github.com/misaon/nylzo/compare/v0.14.0...v0.15.0) (2026-11-27)


### Fixes

* **admin:** resolve the admin and UI kit UX review findings ([#548](https://github.com/misaon/nylzo/issues/548)) ([8720700](https://github.com/misaon/nylzo/commit/872070094162f19675f9ee2dd0032190e5d5ba44))
* **client:** keep the origin of a server address parseable ([#547](https://github.com/misaon/nylzo/issues/547)) ([52ad35a](https://github.com/misaon/nylzo/commit/52ad35a69a00f354a84b5866e3449e3e8a48eec2))
* **core:** refuse a text whose escaped meta outgrows the limit ([#546](https://github.com/misaon/nylzo/issues/546)) ([caa629a](https://github.com/misaon/nylzo/commit/caa629a27696d0cdd20da58ff1d858ea049f6311))
* **web:** make opening a message recoverable and clear ([#549](https://github.com/misaon/nylzo/issues/549)) ([43c1963](https://github.com/misaon/nylzo/commit/43c196307ce6e6188cf8f76050e428eb6eae481f))
* **web:** shorten the French and Polish search snippets ([#541](https://github.com/misaon/nylzo/issues/541)) ([861225b](https://github.com/misaon/nylzo/commit/861225b7f182a9aaf4156ac64fe5fa427829e9dc))
* **web:** UX fixes for Sent, Transparency, status pages and the app shell ([#552](https://github.com/misaon/nylzo/issues/552)) ([824756e](https://github.com/misaon/nylzo/commit/824756e1fdba0437adbfd579587244deb725df98))


### Build

* **infrastructure:** update infrastructure dependencies (main) ([#562](https://github.com/misaon/nylzo/issues/562)) ([3e59a0b](https://github.com/misaon/nylzo/commit/3e59a0baaab1d359d6237c18e90a6f83f9f005a6))
* **infrastructure:** update infrastructure dependencies to v1.13.10 (main) ([#543](https://github.com/misaon/nylzo/issues/543)) ([c0bb5bc](https://github.com/misaon/nylzo/commit/c0bb5bc3ab82ff74fbc14dc260c3635f9f2338d5))
* **infrastructure:** update infrastructure dependencies to v3.1.5 (main) ([#554](https://github.com/misaon/nylzo/issues/554)) ([a98aa75](https://github.com/misaon/nylzo/commit/a98aa75ecb3470f25e0acddfcb373dd545b4cbdc))


### CI

* **deps:** update ci dependencies (main) ([#534](https://github.com/misaon/nylzo/issues/534)) ([77987e5](https://github.com/misaon/nylzo/commit/77987e5aa09033038c4e4083d29a7bcbef9145f1))
* **deps:** update ci dependencies (main) ([#553](https://github.com/misaon/nylzo/issues/553)) ([f833167](https://github.com/misaon/nylzo/commit/f833167deeba51530baf92f9a86301c3de166bcc))
* **deps:** update ci dependencies (main) ([#565](https://github.com/misaon/nylzo/issues/565)) ([ad49167](https://github.com/misaon/nylzo/commit/ad49167fd3a86814bc8e7cb69ed7f6530c570cec))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.261.1 (main) ([#555](https://github.com/misaon/nylzo/issues/555)) ([7ebaf17](https://github.com/misaon/nylzo/commit/7ebaf1720bac255c47e56d85e16a1e2e744cc47a))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.261.2 (main) ([#556](https://github.com/misaon/nylzo/issues/556)) ([991b1b3](https://github.com/misaon/nylzo/commit/991b1b3b00323378b18f4a6d8141615e36ac4afa))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.262.1 (main) ([#558](https://github.com/misaon/nylzo/issues/558)) ([dc25b63](https://github.com/misaon/nylzo/commit/dc25b63936c876bba778933fa9888f24ed66b441))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.262.2 (main) ([#559](https://github.com/misaon/nylzo/issues/559)) ([854cf67](https://github.com/misaon/nylzo/commit/854cf67e6102380d90e14b70cb203b967942dcec))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.263.5 (main) ([#561](https://github.com/misaon/nylzo/issues/561)) ([3e55b72](https://github.com/misaon/nylzo/commit/3e55b7241c27edc90dafdf467493bce5efd0ac21))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.263.6 (main) ([#564](https://github.com/misaon/nylzo/issues/564)) ([64e73b9](https://github.com/misaon/nylzo/commit/64e73b9174974952e8ce5e6abed3d33d3fd1cc7c))
* **deps:** update renovatebot/github-action action to v46.3.15 (main) ([#569](https://github.com/misaon/nylzo/issues/569)) ([43724f1](https://github.com/misaon/nylzo/commit/43724f1b078d3405dcf3f63fdfa09c8386b763ee))


### Tests

* cover the Go and web code close to 100 % ([#542](https://github.com/misaon/nylzo/issues/542)) ([c25a475](https://github.com/misaon/nylzo/commit/c25a47571a990057b9c95bdc5c80b22abdadf941))
* **web:** give the client startup tests time for a cold first boot ([#545](https://github.com/misaon/nylzo/issues/545)) ([e3c40a8](https://github.com/misaon/nylzo/commit/e3c40a88443b7edade372dd93af109ccb593c9cf))


### Maintenance

* **deploy:** validate new servers against v0.14.0 ([#539](https://github.com/misaon/nylzo/issues/539)) ([c2a830e](https://github.com/misaon/nylzo/commit/c2a830e94b21ec45fc005b8969b81c1aa651050e))

## [0.14.0](https://github.com/misaon/nylzo/compare/v0.13.0...v0.14.0) (2026-11-23)


### ⚠ BREAKING CHANGES

* **deploy:** remove the switched-off mTLS host monitor and close port 8443 ([#490](https://github.com/misaon/nylzo/issues/490))
* **security:** widen the pepper id to 8 bytes and settle two passphrase-attempt cases ([#477](https://github.com/misaon/nylzo/issues/477))
* **server:** cap the proof-of-work difficulty at 7 ([#476](https://github.com/misaon/nylzo/issues/476))
* **server:** remove the Scaleway proxy mode ([#456](https://github.com/misaon/nylzo/issues/456))
* self-hosted instances answer X-Robots-Tag: noindex, nofollow on every page and /sitemap.xml with 404; only https://nylzo.com stays indexable.
* the nylzo-e1 seed of earlier releases is erased, not converted.
* **security:** the Compose example needs Nylzo 0.14.0 or later, the tmpfiles entry from nylzo-epoch.conf, and a host without swap or with a noswap tmpfs on /run/nylzo/epoch. A host reboot ends every live message. A deployment that takes the new compose.yaml must first erase epoch/ in nylzo-data in place, as the Self-hosting guide shows.
* **deploy:** the Compose example needs Docker Engine 28.0 or newer. Engine 27 refuses the isolated gateway mode; Engine 26 and older ignore it, which check-isolation reports.
* **security:** the fragment and envelope move to version 3 and the first start of serve erases every message of protocol v2. Clients of protocol v2 get client_outdated and must be updated.
* **security:** messages now seal their deadline (envelope flag 2) and the TTL runs from the instance response. A CLI before this release can neither create nor read such messages and gets client_outdated, and the seed file of an unreleased main build (nylzo-e2) is refused as damaged. Ship server and CLI together.
* **security:** clients from before this release send an X25519 share key and get 400 client_outdated; a client of this release gets 400 bad_request from an older server. Ship server and CLI together.

### Features

* **passphrase:** suggest words of the user's language ([#467](https://github.com/misaon/nylzo/issues/467)) ([f5d598e](https://github.com/misaon/nylzo/commit/f5d598edefb51e447c532bd73facc3ca3a630dc5))
* **security:** check passphrases with OPAQUE in protocol v3 ([#360](https://github.com/misaon/nylzo/issues/360)) ([8dfe6e6](https://github.com/misaon/nylzo/commit/8dfe6e60f78b28c5aab6133c93235f8d2108fa33))
* **server:** add the operator admin area with passkey sign-in and visit counts ([#399](https://github.com/misaon/nylzo/issues/399)) ([2731dfc](https://github.com/misaon/nylzo/commit/2731dfcf36057dac83437b0cd98026837c563955))
* **web:** add press feedback, spring motion, a phone sheet and accessibility modes ([#525](https://github.com/misaon/nylzo/issues/525)) ([41d6954](https://github.com/misaon/nylzo/commit/41d6954a5eb5d01ae2d7469a702212e4307fbe68))
* **web:** add the main search phrase to the home page and the FAQ ([#478](https://github.com/misaon/nylzo/issues/478)) ([a829e54](https://github.com/misaon/nylzo/commit/a829e54fbe66d024f61c003e4bead86fe83212ca))
* **web:** apply the 0.14 design review ([#524](https://github.com/misaon/nylzo/issues/524)) ([a0a5ad3](https://github.com/misaon/nylzo/commit/a0a5ad3a4eaed54246dd8aeb7cdc54e067227de1))
* **web:** link the Terms next to Create link and Show message ([#475](https://github.com/misaon/nylzo/issues/475)) ([d2d451c](https://github.com/misaon/nylzo/commit/d2d451c8fa6f831f8eae919b2a1affc7bfd66074))


### Fixes

* **automation:** enforce the gate's import allowlist without a look-around regex ([#510](https://github.com/misaon/nylzo/issues/510)) ([876ae1f](https://github.com/misaon/nylzo/commit/876ae1fefdec7180a5efb955b24e8cfc409d244c))
* **automation:** hold go.step.sm/crypto until Caddy raises it ([#386](https://github.com/misaon/nylzo/issues/386)) ([cecfeb5](https://github.com/misaon/nylzo/commit/cecfeb5ff486e5656c710045df333d4b11071827))
* **automation:** unblock the quic-go, regctl and Ubuntu 26.04 Arm updates ([#385](https://github.com/misaon/nylzo/issues/385)) ([5b3398d](https://github.com/misaon/nylzo/commit/5b3398d7f53c18d530973e68324bba088bdca27a))
* **backend:** update backend dependencies (main) ([#394](https://github.com/misaon/nylzo/issues/394)) ([6332f00](https://github.com/misaon/nylzo/commit/6332f00d80c3713c58ff53ccf48a2e402a5a243a))
* **backend:** update backend dependencies (main) ([#408](https://github.com/misaon/nylzo/issues/408)) ([319ffbe](https://github.com/misaon/nylzo/commit/319ffbe0bd3c3b352f6dba50d084df87350f86d8))
* **backend:** update backend dependencies (main) ([#512](https://github.com/misaon/nylzo/issues/512)) ([60c9851](https://github.com/misaon/nylzo/commit/60c9851af000a772fe8848b557ebd4b94907719b))
* **backend:** update module github.com/grpc-ecosystem/grpc-gateway/v2 to v2.34.0 (main) ([#517](https://github.com/misaon/nylzo/issues/517)) ([31cdb4c](https://github.com/misaon/nylzo/commit/31cdb4c1b42e352da693d7f7e05604e8c350b2eb))
* **backend:** update module golang.org/x/crypto to v0.59.0 [security] (main) ([#338](https://github.com/misaon/nylzo/issues/338)) ([3b05d77](https://github.com/misaon/nylzo/commit/3b05d77dffd99082ab10cdeb7b53314f13b51785))
* **backend:** update OpenTelemetry to v1.47.0 in the Caddy build ([#377](https://github.com/misaon/nylzo/issues/377)) ([2ec368c](https://github.com/misaon/nylzo/commit/2ec368c8f93da6db77cd8b73ac533be74931b1d1))
* **ci:** check pinned actions in composite actions for advisories ([#454](https://github.com/misaon/nylzo/issues/454)) ([bbbcda3](https://github.com/misaon/nylzo/commit/bbbcda351ea79eb1ea03f7c5ea694f76dc077b79))
* **ci:** let Dependency guard accept registry:3 digest refreshes ([#453](https://github.com/misaon/nylzo/issues/453)) ([231c4b0](https://github.com/misaon/nylzo/commit/231c4b0616769d9b2d05bddc722aac9355880ec1))
* **ci:** let the vulnerability inventory read releases without composite actions ([#527](https://github.com/misaon/nylzo/issues/527)) ([6f29112](https://github.com/misaon/nylzo/commit/6f291128a03b794d84b62d7023874345ab920071))
* **cli:** end by the stop signal after cleanup ([#353](https://github.com/misaon/nylzo/issues/353)) ([2f95b98](https://github.com/misaon/nylzo/commit/2f95b98623ef198c7c5cbeb4839b558ef387e5b9))
* **cli:** make the read --out directory after the passphrase prompt ([#358](https://github.com/misaon/nylzo/issues/358)) ([5f50c39](https://github.com/misaon/nylzo/commit/5f50c39c05c45be66b81a4938b5cd30b801d6d2f))
* correct the 0.14.0 upgrade advice and derive the Compose Caddyfile ([#465](https://github.com/misaon/nylzo/issues/465)) ([dacfaf0](https://github.com/misaon/nylzo/commit/dacfaf00079a8e59f8e7b3b2fd5d7731012ad0be))
* **deploy:** enforce the encrypted data volume in the strict profile (F20) ([#378](https://github.com/misaon/nylzo/issues/378)) ([8d3e62d](https://github.com/misaon/nylzo/commit/8d3e62ddbfa104172b68dd5e469170207c03bc33))
* **deploy:** isolate the Compose backend from the internet and the host (F19) ([#379](https://github.com/misaon/nylzo/issues/379)) ([26bb995](https://github.com/misaon/nylzo/commit/26bb99525ce75ea6675d69f3617db7c2416acebd))
* **deploy:** keep stored messages open across a pepper rotation on Hetzner ([#348](https://github.com/misaon/nylzo/issues/348)) ([df1934f](https://github.com/misaon/nylzo/commit/df1934f24e48d67d04781acbb0bbab0764906c33))
* end messages a host reboot left unreadable as lost (F21) ([#381](https://github.com/misaon/nylzo/issues/381)) ([67f0dcd](https://github.com/misaon/nylzo/commit/67f0dcd8dc192578879828f910bd718e3faed081))
* **frontend:** update frontend dependencies (main) ([#393](https://github.com/misaon/nylzo/issues/393)) ([6fcf0a2](https://github.com/misaon/nylzo/commit/6fcf0a2ff9799e31bace792460e973148a3aa140))
* **frontend:** update frontend dependencies (main) ([#406](https://github.com/misaon/nylzo/issues/406)) ([89f2d94](https://github.com/misaon/nylzo/commit/89f2d94538904e0698420ca335f6cbed403bb956))
* **frontend:** update frontend dependencies (main) ([#513](https://github.com/misaon/nylzo/issues/513)) ([048472e](https://github.com/misaon/nylzo/commit/048472e2dd9cc006bfdc360ccb9a7d702cd210b0))
* let search and AI crawlers reach nylzo.com and index only its home pages and images ([#403](https://github.com/misaon/nylzo/issues/403)) ([b2265e9](https://github.com/misaon/nylzo/commit/b2265e9f5952a7528e6a1d1f5bb851b574dacc11))
* **passphrase:** treat every kind of space in a passphrase as one space ([#496](https://github.com/misaon/nylzo/issues/496)) ([119a3ac](https://github.com/misaon/nylzo/commit/119a3ac9d4b9f24547cc83a22d26b588ecad7a2a))
* refresh crawler ranges per source and check that nylzo.com stays indexable ([#404](https://github.com/misaon/nylzo/issues/404)) ([12bed6f](https://github.com/misaon/nylzo/commit/12bed6f5bd9a2610c4bcb5380e909ade047d6e6a))
* **security:** ask before every attachment download ([#341](https://github.com/misaon/nylzo/issues/341)) ([d280a57](https://github.com/misaon/nylzo/commit/d280a57705cc9ec2a3b65d759a3513873ccde258))
* **security:** bind share and lease headers to request signatures ([#330](https://github.com/misaon/nylzo/issues/330)) ([82f5303](https://github.com/misaon/nylzo/commit/82f53037ae27338058534b57a51489fb226cdbdc))
* **security:** check a tag over every stored blob before spending an open ([#364](https://github.com/misaon/nylzo/issues/364)) ([50d63dd](https://github.com/misaon/nylzo/commit/50d63ddd79c77c474c23d08a57863ba00af6bd94))
* **security:** end the expiry share at the message deadline ([#366](https://github.com/misaon/nylzo/issues/366)) ([344b8ff](https://github.com/misaon/nylzo/commit/344b8fff5104eded8a172ccd0f3c8aca8804e535))
* **security:** harden CLI processes before they read secrets ([#337](https://github.com/misaon/nylzo/issues/337)) ([ffa0667](https://github.com/misaon/nylzo/commit/ffa06676ba054e03fb42551c4a73ce82a3e10199))
* **security:** keep passphrase verifiers out of every log ([#357](https://github.com/misaon/nylzo/issues/357)) ([a0aec34](https://github.com/misaon/nylzo/commit/a0aec34ea816a106d99787a0cd768045c8ea533b))
* **security:** keep read running until a claimed message is saved ([#349](https://github.com/misaon/nylzo/issues/349)) ([191213e](https://github.com/misaon/nylzo/commit/191213e2773204f81ded53f06c6c7d3da7231d4a))
* **security:** keep request challenges in memory, out of the database ([#335](https://github.com/misaon/nylzo/issues/335)) ([9b1059f](https://github.com/misaon/nylzo/commit/9b1059f488af10e39cd48e684cc503df7eff086e))
* **security:** keep the Compose epoch seed on host tmpfs (F18) ([#380](https://github.com/misaon/nylzo/issues/380)) ([1414f10](https://github.com/misaon/nylzo/commit/1414f106c7c54b7d43f68c8073c99a473550aa6c))
* **security:** let the dependency gate accept build-only npm packages ([#451](https://github.com/misaon/nylzo/issues/451)) ([c17f2ca](https://github.com/misaon/nylzo/commit/c17f2ca19f7d4f7a84f3620d32031aa31369378a))
* **security:** mark files that read writes as downloaded from the internet ([#344](https://github.com/misaon/nylzo/issues/344)) ([a9f34b0](https://github.com/misaon/nylzo/commit/a9f34b0aaa41fb1676bcd26ea714cd56d985d050))
* **security:** rebuild with Go 1.27.4 ([#327](https://github.com/misaon/nylzo/issues/327)) ([d790cbe](https://github.com/misaon/nylzo/commit/d790cbee51609a5ecc37271a0ae83295a883e7ff))
* **security:** renew epoch seeds with fresh entropy every hour ([#354](https://github.com/misaon/nylzo/issues/354)) ([3094e54](https://github.com/misaon/nylzo/commit/3094e5489f5ac5c1b398f031ef1579323c053a6e))
* **security:** replace an epoch generation that a clock running ahead started ([#464](https://github.com/misaon/nylzo/issues/464)) ([7050cf2](https://github.com/misaon/nylzo/commit/7050cf26ffcc9d23fd536f043dd45da9c66d38f5))
* **security:** require the passphrase to delete a protected message ([#340](https://github.com/misaon/nylzo/issues/340)) ([591dcbb](https://github.com/misaon/nylzo/commit/591dcbb5a9f018be4c6199166f55377388c748ce))
* **security:** seal record key wraps with a key from the pepper ([#347](https://github.com/misaon/nylzo/issues/347)) ([8541282](https://github.com/misaon/nylzo/commit/854128268c694d8036f35530769b77fe83147ba9))
* **security:** sign macOS release binaries with the hardened runtime ([#345](https://github.com/misaon/nylzo/issues/345)) ([34c8477](https://github.com/misaon/nylzo/commit/34c847746d0ee9de7ee97db2abd556a0a9f1b7af))
* **security:** warn after read saves an attachment ([#346](https://github.com/misaon/nylzo/issues/346)) ([35d2473](https://github.com/misaon/nylzo/commit/35d24733f9226775fb0fbaf34797edba4d7d023c))
* **security:** widen the pepper id to 8 bytes and settle two passphrase-attempt cases ([#477](https://github.com/misaon/nylzo/issues/477)) ([5996cd2](https://github.com/misaon/nylzo/commit/5996cd2161d1b890aaafc3ff37b71d2e30c284c8))
* **security:** wrap expiry shares with hybrid post-quantum HPKE ([#361](https://github.com/misaon/nylzo/issues/361)) ([4894da7](https://github.com/misaon/nylzo/commit/4894da7ea544c4d9e1e2f588fb4d26c1f52b2539))
* **security:** write private CLI files owner-only on local disks ([#336](https://github.com/misaon/nylzo/issues/336)) ([5f4c20f](https://github.com/misaon/nylzo/commit/5f4c20f22bdf05ad2396cc95a82d68b2e09ef965))
* **server:** cap the proof-of-work difficulty at 7 ([#476](https://github.com/misaon/nylzo/issues/476)) ([512634f](https://github.com/misaon/nylzo/commit/512634f9cf07ebb8393f8c9257a5b19ffcc3dffe))
* **server:** count each ended message once and send only new dashboard samples ([#466](https://github.com/misaon/nylzo/issues/466)) ([b0d2875](https://github.com/misaon/nylzo/commit/b0d28752a05c7404746c4a41af50ea15c0130772))
* **server:** settle the low findings of the admin sign-in review ([#472](https://github.com/misaon/nylzo/issues/472)) ([b620be6](https://github.com/misaon/nylzo/commit/b620be6385d3a22d065d4ce1c6708f064f51ce7b))
* **store:** report a WAL scrub that another writer outgrew as busy ([#470](https://github.com/misaon/nylzo/issues/470)) ([95f1196](https://github.com/misaon/nylzo/commit/95f1196932e52f9ca02121a4f7e85b354b1a03b8))
* **web:** copy hidden characters, not their labels, when a reader copies by hand ([#505](https://github.com/misaon/nylzo/issues/505)) ([f66e1ac](https://github.com/misaon/nylzo/commit/f66e1acbfd30676785ff13519da530a764ed722c))
* **web:** give Google Search a bitmap favicon and serve /favicon.ico ([#400](https://github.com/misaon/nylzo/issues/400)) ([86d7467](https://github.com/misaon/nylzo/commit/86d7467ab168ff1b866385a7605cb8ce482109e6))
* **web:** give the card its final size at the Create link click ([#483](https://github.com/misaon/nylzo/issues/483)) ([0f5476f](https://github.com/misaon/nylzo/commit/0f5476f18805568aef9da65bb7a5b1984a5274be))
* **web:** glide the read card up when a message ends instead of collapsing the intro ([#506](https://github.com/misaon/nylzo/issues/506)) ([2b3bbc4](https://github.com/misaon/nylzo/commit/2b3bbc4f663d70772d4fc447994f8e60327a2127))
* **web:** keep a late passphrase suggestion out of a field the sender reached ([#484](https://github.com/misaon/nylzo/issues/484)) ([b80f5fd](https://github.com/misaon/nylzo/commit/b80f5fd8c52da36d767397d22254738b3f5b1af5))
* **web:** keep the FAQ order on phones, report copy failures, use Button for icon buttons ([#492](https://github.com/misaon/nylzo/issues/492)) ([0dfcf23](https://github.com/misaon/nylzo/commit/0dfcf23d6fc459add48aaa8a7451db23f3404ea7))
* **web:** keep the read card and its bar in place when a message is revealed ([#500](https://github.com/misaon/nylzo/issues/500)) ([d2f325e](https://github.com/misaon/nylzo/commit/d2f325e9160c0c226ab18220089426ff1d82a08c))
* **web:** keep tracked sent messages in the tab by default (F16) ([#365](https://github.com/misaon/nylzo/issues/365)) ([2d4ce75](https://github.com/misaon/nylzo/commit/2d4ce75021fe839de0fabbd36e0f16f059d196c2))
* **web:** move nothing when a message ends after a slow answer ([#511](https://github.com/misaon/nylzo/issues/511)) ([f16e0ba](https://github.com/misaon/nylzo/commit/f16e0ba9e04bf31f16efcac3f4b127752260b4ce))
* **web:** narrow the claim that a quantum computer cannot read a message ([#463](https://github.com/misaon/nylzo/issues/463)) ([da52140](https://github.com/misaon/nylzo/commit/da52140c2cf5825d43376b6175aac4c4d0371b85))
* **web:** remove third-party analytics from message forms ([#329](https://github.com/misaon/nylzo/issues/329)) ([7161e40](https://github.com/misaon/nylzo/commit/7161e40376568f1a0ad9caac5ea73598386e7d6f))
* **web:** say in the Terms where to report unlawful content ([#455](https://github.com/misaon/nylzo/issues/455)) ([d6fe27a](https://github.com/misaon/nylzo/commit/d6fe27aac3805fe7b0e554544b63fb474f5f431c))
* **web:** show an error when the app fails to start and keep the draft after a language switch ([#479](https://github.com/misaon/nylzo/issues/479)) ([5762297](https://github.com/misaon/nylzo/commit/5762297c5e4e491699be08f004e3b13b811462e9))
* **web:** show the saved note and the Sent link only after the tracking panel has left ([#509](https://github.com/misaon/nylzo/issues/509)) ([2b67e42](https://github.com/misaon/nylzo/commit/2b67e42ec94c3fbbc34a0433f07b9e6572674dbd))
* **web:** show the whole revealed passphrase after a message is created ([#494](https://github.com/misaon/nylzo/issues/494)) ([3b45802](https://github.com/misaon/nylzo/commit/3b45802d2849d15324b569eda915c7dc80887214))
* **web:** simplify the Svelte client and correct the build targets ([#389](https://github.com/misaon/nylzo/issues/389)) ([6629645](https://github.com/misaon/nylzo/commit/66296456582b9c9352a07c77f4f885a07e57386f))
* **web:** smooth the remaining animation jumps ([#497](https://github.com/misaon/nylzo/issues/497)) ([e013011](https://github.com/misaon/nylzo/commit/e013011f6c0a43abcc43cfe54b75386db162d9fc))
* **web:** start the passphrase field shown for Japanese and Chinese readers ([#493](https://github.com/misaon/nylzo/issues/493)) ([ced6d06](https://github.com/misaon/nylzo/commit/ced6d0649af91b577f12278bb21e3d862f420baa))


### Performance

* **web:** cache the verified KDF worker and serve the og-image as JPEG ([#460](https://github.com/misaon/nylzo/issues/460)) ([822cfc8](https://github.com/misaon/nylzo/commit/822cfc8fecd238736bcd3eae7611e53e052a3958))
* **web:** make every page measurable and cut work on load and interaction ([#405](https://github.com/misaon/nylzo/issues/405)) ([e1e990a](https://github.com/misaon/nylzo/commit/e1e990a56d1293e2c94fe74da127b35df16c5839))
* **web:** shorten the minimum step times and prerender a page skeleton ([#481](https://github.com/misaon/nylzo/issues/481)) ([7d16c81](https://github.com/misaon/nylzo/commit/7d16c8134ef7d6dbefe339cf19afe68ead359670))


### Documentation

* give svelte-ignore reasons, fix a stale lint comment and the dependency count ([#457](https://github.com/misaon/nylzo/issues/457)) ([78c6c64](https://github.com/misaon/nylzo/commit/78c6c64018563c2f57e37aae9bead808ef112a26))
* **wiki:** rewrite the security assessment for protocol v3 ([#487](https://github.com/misaon/nylzo/issues/487)) ([bcedd1c](https://github.com/misaon/nylzo/commit/bcedd1c3e4ab6e48de90112445b72fdd9dde1e38))
* **wiki:** rewrite the wiki for its readers ([#450](https://github.com/misaon/nylzo/issues/450)) ([9e1486d](https://github.com/misaon/nylzo/commit/9e1486d9c959d6e095817281a70095c44a19c6d8))


### Refactoring

* apply the TypeScript best-practices rules across the repository ([#526](https://github.com/misaon/nylzo/issues/526)) ([330d00a](https://github.com/misaon/nylzo/commit/330d00a6030a7c296d425fe1d9cd5b4621eaf78b))
* **deploy:** remove the switched-off mTLS host monitor and close port 8443 ([#490](https://github.com/misaon/nylzo/issues/490)) ([481fbc8](https://github.com/misaon/nylzo/commit/481fbc88432c01b316a521144d8bd088858ea04d))
* remove abuse reports and the formats 0.14.0 no longer meets ([#390](https://github.com/misaon/nylzo/issues/390)) ([2155100](https://github.com/misaon/nylzo/commit/21551000a6cae35f4b2e80ffb140bd9b89ef6324))
* remove unused OPAQUE session keys and dead return values ([#468](https://github.com/misaon/nylzo/issues/468)) ([5591b14](https://github.com/misaon/nylzo/commit/5591b14bdb79b62f0543862a4f282729af493342))
* replace retired decision ids outside the web client ([#486](https://github.com/misaon/nylzo/issues/486)) ([3681094](https://github.com/misaon/nylzo/commit/368109469688d002c106c276dc1a88d34fd60e16))
* **server:** remove the Scaleway proxy mode ([#456](https://github.com/misaon/nylzo/issues/456)) ([daf543d](https://github.com/misaon/nylzo/commit/daf543d95c970ca21abcb61c4296053568203f78))
* simplify the Go code without changing behaviour ([#387](https://github.com/misaon/nylzo/issues/387)) ([dd3d997](https://github.com/misaon/nylzo/commit/dd3d99763f992a082c2d4661c1008dcc3624a683))
* **web:** replace retired decision ids in the web client ([#498](https://github.com/misaon/nylzo/issues/498)) ([36ee14c](https://github.com/misaon/nylzo/commit/36ee14c04b123e669839ece27cf29724a5ec3d48))


### Build

* **deps:** drop the picomatch assessment after the advisory was withdrawn ([#488](https://github.com/misaon/nylzo/issues/488)) ([5367f87](https://github.com/misaon/nylzo/commit/5367f870382d991ccd4080361d65604d4de60b0b))
* **deps:** hold whatwg-url below 0.8.0 until go-urlpattern follows its rename ([#480](https://github.com/misaon/nylzo/issues/480)) ([2bfe2a8](https://github.com/misaon/nylzo/commit/2bfe2a85a93e69b73ae3a8b82198baf66a71327c))
* **deps:** remove the deepmerge patch after the advisory was withdrawn ([#452](https://github.com/misaon/nylzo/issues/452)) ([5962503](https://github.com/misaon/nylzo/commit/59625035d6840da9143ccd02ce049dfc1cdd7411))
* **go:** make CLI code create files through internal/privatefile ([#462](https://github.com/misaon/nylzo/issues/462)) ([f2cd4dc](https://github.com/misaon/nylzo/commit/f2cd4dc07060e89d5669d8238b69a33e5ad1d76d))
* **infrastructure:** update infrastructure dependencies (main) ([#350](https://github.com/misaon/nylzo/issues/350)) ([4ccfaac](https://github.com/misaon/nylzo/commit/4ccfaac207905439bfc9b841a4dbcc74a703d47a))
* **infrastructure:** update infrastructure dependencies to v2.105.0 (main) ([#407](https://github.com/misaon/nylzo/issues/407)) ([61ffb9d](https://github.com/misaon/nylzo/commit/61ffb9db96bc16918540b6f5e8781206e148b20b))
* **infrastructure:** update infrastructure dependencies to v2.20.1 (main) ([#324](https://github.com/misaon/nylzo/issues/324)) ([1e64ec5](https://github.com/misaon/nylzo/commit/1e64ec5237d3ab8df9d125f3fcfbd868ec8068e5))
* **infrastructure:** update infrastructure dependencies to v2.20.3 (main) ([#516](https://github.com/misaon/nylzo/issues/516)) ([05039c6](https://github.com/misaon/nylzo/commit/05039c640df6245ce45008ae5f7adb1ad85a4d60))
* **infrastructure:** update ubuntu docker tag to v26 (main) ([#395](https://github.com/misaon/nylzo/issues/395)) ([8885c0d](https://github.com/misaon/nylzo/commit/8885c0df665dadaa6c3c0f9df6e955859a19c0c4))
* **infrastructure:** update ubuntu:24.04 docker digest to 2198e4c (main) ([#391](https://github.com/misaon/nylzo/issues/391)) ([fa10d03](https://github.com/misaon/nylzo/commit/fa10d035aed54b03bbc609121bbfc92bc079869a))
* **web:** drop perfectionist, run client tests in happy-dom, share one language table ([#461](https://github.com/misaon/nylzo/issues/461)) ([7e0df91](https://github.com/misaon/nylzo/commit/7e0df913965eb17e9f62c89f7b7d0682f4970655))


### CI

* block unlisted network traffic in jobs that hold secrets ([#459](https://github.com/misaon/nylzo/issues/459)) ([224c391](https://github.com/misaon/nylzo/commit/224c391cd021083d58204444c4efb5e45544a475))
* cache the browser packages that apt downloads for the e2e jobs ([#491](https://github.com/misaon/nylzo/issues/491)) ([3a44609](https://github.com/misaon/nylzo/commit/3a4460985dae930315d17caf146965caee64c529))
* **deps:** replace actions/attest-build-provenance action with actions/attest v4.2.2 (main) ([#279](https://github.com/misaon/nylzo/issues/279)) ([52beb8a](https://github.com/misaon/nylzo/commit/52beb8a1cef9a95d7ff58d960c6174e964c00997))
* **deps:** update ci dependencies (main) ([#317](https://github.com/misaon/nylzo/issues/317)) ([ae9db4b](https://github.com/misaon/nylzo/commit/ae9db4b6287943284555149036d275579ce7ea2d))
* **deps:** update ci dependencies (main) ([#351](https://github.com/misaon/nylzo/issues/351)) ([eaab2ce](https://github.com/misaon/nylzo/commit/eaab2ce4fcde0e707abcdd76446611f4efa60d0c))
* **deps:** update ci dependencies (main) ([#356](https://github.com/misaon/nylzo/issues/356)) ([8beb6a0](https://github.com/misaon/nylzo/commit/8beb6a039dab02f284bd6c4469c4c6497de52252))
* **deps:** update ci dependencies (main) ([#370](https://github.com/misaon/nylzo/issues/370)) ([2ec41d0](https://github.com/misaon/nylzo/commit/2ec41d0ba8f8b625f33878a4bce45608963d3a62))
* **deps:** update ci dependencies (main) ([#514](https://github.com/misaon/nylzo/issues/514)) ([093df11](https://github.com/misaon/nylzo/commit/093df119800bc5ca01142a6b77f6c3b84031c24a))
* **deps:** update ci dependencies (main) ([#531](https://github.com/misaon/nylzo/issues/531)) ([d970612](https://github.com/misaon/nylzo/commit/d970612b3c7b428ac985bc8efd0cb87a80ce5523))
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
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.257.0 (main) ([#521](https://github.com/misaon/nylzo/issues/521)) ([b181bf7](https://github.com/misaon/nylzo/commit/b181bf7cc5355800c28efced81738b2ae96c380c))
* **deps:** update ghcr.io/renovatebot/renovate docker tag to v44.257.5 (main) ([#523](https://github.com/misaon/nylzo/issues/523)) ([d255f26](https://github.com/misaon/nylzo/commit/d255f266bf9b9b9284ef22b6e87f3db550994819))
* fold the Windows test steps and drop the redundant CodeQL autobuild ([#388](https://github.com/misaon/nylzo/issues/388)) ([eca2fcb](https://github.com/misaon/nylzo/commit/eca2fcb25d3cdf2ea7983bf3b9de3e833edb6791))
* **security:** isolate native release builds from publication ([#331](https://github.com/misaon/nylzo/issues/331)) ([26dfa48](https://github.com/misaon/nylzo/commit/26dfa482d29ecbe09f22bf5ed1f6069bda2d7ccd))
* type-check and lint the scripts in tools/ ([#458](https://github.com/misaon/nylzo/issues/458)) ([a6fa9ae](https://github.com/misaon/nylzo/commit/a6fa9ae64bf90bec93850616700bf008095ded14))


### Tests

* **cli:** check on Windows that Edge marks an attachment from nylzo open ([#471](https://github.com/misaon/nylzo/issues/471)) ([7f107fd](https://github.com/misaon/nylzo/commit/7f107fde3e192e555daaa220a60935beb5cccfd2))
* **guard:** derive the newer override patch from the locked version ([#383](https://github.com/misaon/nylzo/issues/383)) ([366c08c](https://github.com/misaon/nylzo/commit/366c08c8a248fc85ffddf7344a50df533375c666))
* **web:** fail the build above the compressed size budget ([#499](https://github.com/misaon/nylzo/issues/499)) ([cfc8d93](https://github.com/misaon/nylzo/commit/cfc8d93cd30ade90ec9caac20e9ce5285bd083a7))
* **web:** ignore the rejection happy-dom raises for a cancelled animation ([#520](https://github.com/misaon/nylzo/issues/520)) ([2fa7fb9](https://github.com/misaon/nylzo/commit/2fa7fb969ad15208aa542942b1a9a45e19f768fe))
* **web:** open Sent from the header in the second round of the tab-list test ([#489](https://github.com/misaon/nylzo/issues/489)) ([97427ea](https://github.com/misaon/nylzo/commit/97427ea72a37ea7f6434a091703e6b177d09a570))
* **web:** wait for the tracking panel to leave before a test clicks under it ([#507](https://github.com/misaon/nylzo/issues/507)) ([1db206c](https://github.com/misaon/nylzo/commit/1db206cd6a40ac6ca7f9f07dfdd27ae2a114fb92))


### Maintenance

* **automation:** remove the pnpm and Node.js version rules ([#528](https://github.com/misaon/nylzo/issues/528)) ([9218ce8](https://github.com/misaon/nylzo/commit/9218ce8c441dc86ce8394ddf9cb17c9b44a0fe4d))
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
