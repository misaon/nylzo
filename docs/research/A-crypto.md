# Crypto facts for a one-time-link secret sharing app

Research date: 2026-10-21. Every claim below was checked by opening the page (WebFetch, or curl + jq / pypdf for JSON and PDF sources). Status values: VERIFIED (read on the page), PARTIAL (page found, does not say exactly this), NOT FOUND (what was tried is stated). Quotes are copied from the page; where a PDF text extraction dropped superscripts the intended exponent is given in brackets.

## 1. Web Crypto API support

- Claim: MDN's SubtleCrypto algorithm table lists AES-GCM (encrypt/decrypt), HKDF and PBKDF2 (deriveBits/deriveKey), ECDH, X25519 (deriveBits/deriveKey) and Ed25519 (sign/verify). The compat table itself is rendered client side and was not readable through fetch; versions below come from the underlying browser-compat-data file.
  - URL: https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto
  - Quote: "It's been available across browsers since September 2017." (page last modified Sep 12, 2025)
  - Status: VERIFIED (algorithm list); compat versions taken from BCD below

- Claim: Per mdn/browser-compat-data 8.1.6, `encrypt`/`decrypt`/`generateKey`/`importKey` have been available since Chrome 37, Firefox 34, Safari 7 (Safari iOS mirrors Safari); `deriveKey`/`deriveBits` since Chrome 41, Firefox 34, Safari 11; Node 15.0.0 for all of these. BCD has no per-base-algorithm rows for AES-GCM, HKDF or PBKDF2 inside `encrypt`/`deriveKey`; the `derivedKeyAlgorithm_option_hkdf`/`_pbkdf2` rows (Chrome 41, Firefox 119, Safari 11) cover using HKDF/PBKDF2 as the *output* key type, which this design does not need.
  - URL: https://raw.githubusercontent.com/mdn/browser-compat-data/main/api/SubtleCrypto.json
  - Quote: `"chrome":{"version_added":"41"} ... "firefox":{"version_added":"119"} ... "safari":{"version_added":"11"}` (deriveKey HKDF option row)
  - Status: VERIFIED (data file read with jq)

- Claim: X25519 in Web Crypto: Chrome 133, Firefox 130, Safari 17 (iOS mirrors), Node 16.17.0, Deno 1.26. Ed25519: Chrome 137, Firefox 129, Safari 17, Node 16.17.0.
  - URL: https://raw.githubusercontent.com/mdn/browser-compat-data/main/api/SubtleCrypto.json
  - Quote: `"chrome":{"version_added":"133"},"deno":{"version_added":"1.26"},"firefox":{"version_added":"130"},"nodejs":{"version_added":"16.17.0"},"safari":{"version_added":"17"}`
  - Status: VERIFIED

- Claim: Chrome 133 (stable February 4, 2025) shipped X25519 in Web Crypto; Chrome 137 (May 27, 2025) shipped Ed25519.
  - URL: https://developer.chrome.com/release-notes/133 and https://developer.chrome.com/release-notes/137
  - Quote (133): "The "X25519" algorithm provides tools to perform key agreement using the X25519 function specified in [RFC7748]."
  - Quote (137): "This feature adds support for Curve25519 algorithms in the Web Cryptography API, namely the signature algorithm Ed25519"
  - Status: VERIFIED

- Claim: Firefox 130 (September 3, 2024) shipped both Curve25519 primitives.
  - URL: https://www.firefox.com/en-US/firefox/130.0/releasenotes/
  - Quote: "WebCryptoAPI now supports Curve25519 primitives (Ed25519 signatures and X25519 key generation)."
  - Status: VERIFIED

- Claim: Safari 17.0 (September 18, 2023) shipped Ed25519. X25519 in Safari 17 is stated by BCD only; the Apple release-notes page body is script rendered and could not be read.
  - URL: https://webkit.org/blog/14445/webkit-features-in-safari-17-0/ (tried also https://developer.apple.com/documentation/safari-release-notes/safari-17-release-notes)
  - Quote: "Support for Ed25519 cryptography."
  - Status: VERIFIED for Ed25519; PARTIAL for X25519

- Claim: caniuse reports Web Cryptography at 97.48% global support (97.27% full, 0.21% partial); Chrome 37+, Firefox 34+, Safari 7.1+, iOS Safari 8+.
  - URL: https://caniuse.com/cryptography
  - Quote: "Many browsers support the crypto.getRandomValues() method, but not actual cryptography functionality under crypto.subtle."
  - Status: VERIFIED

- Claim: Ed25519 and X25519 are now in the main W3C Web Cryptography spec (Editor's Draft, 11 August 2026, sections 25 and 26). Argon2 and ChaCha20-Poly1305 are not in that spec.
  - URL: https://w3c.github.io/webcrypto/
  - Quote: Table of contents lists "25. Ed25519" and "26. X25519"; status line "W3C Editor's Draft" "11 August 2026"
  - Status: VERIFIED

- Claim: ChaCha20-Poly1305 and Argon2 exist only in the WICG "Modern Algorithms in the Web Cryptography API" draft (Draft Community Group Report, 14 September 2026), together with ML-KEM, hybrid KEMs, ML-DSA, SLH-DSA, AES-OCB, SHA-3, cSHAKE, TurboSHAKE, KangarooTwelve and KMAC. Ed25519/X25519 are not part of this proposal. Repo's last commit: 2026-09-14 "Define JWK for ML-KEM (#64)".
  - URL: https://wicg.github.io/webcrypto-modern-algos/ ; https://github.com/WICG/webcrypto-modern-algos ; https://api.github.com/repos/WICG/webcrypto-modern-algos/commits?per_page=1
  - Quote: "This is an unofficial proposal."; abstract lists "ChaCha20-Poly1305, SHA-3, cSHAKE, TurboSHAKE, KangarooTwelve, KMAC, and Argon2."
  - Status: VERIFIED

- Claim: Shipping status of the modern algorithms: Node implements ChaCha20-Poly1305, ML-KEM (v24.7.0) and Argon2 (v24.8.0) under stability 1.1; Cloudflare Workers implement ML-DSA and ML-KEM-768/1024 behind a compatibility flag and nothing else from the draft; BCD lists `encapsulateKey`/`decapsulateKey` at Chrome 155, Firefox "preview", Safari false. Chrome 155 release notes do not exist yet (404 on research date), so ML-KEM in Chrome Web Crypto is not stable today. No browser ships ChaCha20-Poly1305 or Argon2 in Web Crypto.
  - URL: https://nodejs.org/api/webcrypto.html ; https://developers.cloudflare.com/workers/runtime-apis/web-crypto/ ; BCD file above; https://developer.chrome.com/release-notes/155 (404)
  - Quote (Node): "v24.8.0 — Argon2 algorithms are now supported."; (Workers): "Workers does not implement other algorithms from the draft."
  - Status: VERIFIED for Node and Workers; PARTIAL for Chrome 155

## 2. AES-GCM nonce guidance (NIST SP 800-38D)

- Claim: SP 800-38D is Final (November 2007). NIST decided in March 2024 to revise it; no draft revision has been published (the r1/ipd URL returns 404). Planned changes: drop tags shorter than 96 bits, clarify IV constructions, confirm TLS 1.3 IV construction.
  - URL: https://csrc.nist.gov/pubs/sp/800/38/d/final ; https://csrc.nist.gov/News/2024/nist-to-revise-sp-80038d-gcm-and-gmac-modes (last updated March 11, 2025) ; https://csrc.nist.gov/pubs/sp/800/38/d/r1/ipd (404)
  - Quote: "NIST has decided to revise this publication."; "NIST will request further public comments when the draft revision is released."
  - Status: VERIFIED

- Claim: NIST recommends 96-bit IVs.
  - URL: https://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf (Sec. 5.2.1.1)
  - Quote: "For IVs, it is recommended that implementations restrict support to the length of 96 bits, to promote interoperability, efficiency, and simplicity of design."
  - Status: VERIFIED (PDF text extracted with pypdf)

- Claim: Maximum plaintext per invocation (per key+IV) is 2^39 − 256 bits (about 64 GiB); AAD up to 2^64 − 1 bits.
  - URL: same PDF, Sec. 5.2.1.1
  - Quote: "len(P) ≤ 239-256; • len(A) ≤ 264-1; • 1 ≤ len(IV) ≤ 264-1." [superscripts lost: 2^39−256, 2^64−1]
  - Status: VERIFIED

- Claim: The uniqueness requirement is probabilistic: the chance of any (key, IV) repeat across all invocations must stay at or below 2^-32.
  - URL: same PDF, Sec. 8
  - Quote: "invoked with the same IV and the same key on two (or more) distinct sets of input data shall be no greater than 2-32." [2^-32]
  - Status: VERIFIED

- Claim: With random (RBG-based) IVs the random field must be at least 96 bits and the total number of encryptions under one key must not exceed 2^32.
  - URL: same PDF, Sec. 8.2.2 and 8.3
  - Quote: "the length of the random field shall be at least 96 bits; the free field may be empty."; "The total number of invocations of the authenticated encryption function shall not exceed 232, including all IV lengths" [2^32]
  - Status: VERIFIED

- Claim: The IRTF CFRG usage-limits draft (draft-irtf-cfrg-aead-limits-13, 3 September 2026) fixes the analysis to 96-bit nonces, gives AES-GCM bounds as formulas rather than a flat 2^32, and gives ChaCha20-Poly1305 a confidentiality advantage of zero (limit only from Poly1305 forgery bound).
  - URL: https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-aead-limits
  - Quote: "These algorithms, as cited, all define a nonce length (r) of 96 bits."; AES-GCM: "q + s <= p^(1/2) * 2^(129/2) - 1"; ChaCha20-Poly1305: "CA <= 0"
  - Status: VERIFIED (XChaCha20-Poly1305 is not covered by this draft)

- Reasoning (not NIST's words): with a fresh random 256-bit content key per message, each key encrypts only a handful of chunks (a 5 MB file in 64 KiB chunks is about 80 invocations). The 2^32 limit and the 2^-32 collision budget are per key, so a random 96-bit IV per chunk gives a collision probability of roughly n^2 / 2^97, around 2^-84 for 80 chunks. A deterministic counter IV within the key makes it exactly zero and is simpler to reason about. Either way nonce collision is not a practical concern for this design; the real constraint is never reusing a content key across messages.

## 3. Passphrase key derivation

- Claim: RFC 9106 (September 2021, Informational, IRTF CFRG) makes Argon2id the mandatory variant and gives two recommended parameter sets.
  - URL: https://www.rfc-editor.org/rfc/rfc9106.html
  - Quote: "Argon2id MUST be supported by any implementation of this document, whereas Argon2d and Argon2i MAY be supported."
  - Quote (first option): "select Argon2id with t=1 iteration, p=4 lanes, m=2^(21) (2 GiB of RAM), 128-bit salt, and 256-bit tag size."
  - Quote (second option): "If much less memory is available, a uniformly safe option is" Argon2id t=3, p=4, m=2^(16) (64 MiB)
  - Status: VERIFIED

- Claim: OWASP Password Storage Cheat Sheet (file last changed 2026-10-04, commit "Clarify Argon2 memory and parallelism parameters (#2530)") recommends Argon2id m=19 MiB, t=2, p=1 as the minimum, with equivalents m=47104/t=1, m=12288/t=3, m=9216/t=4, m=7168/t=5; scrypt N=2^17, r=8, p=1; PBKDF2-HMAC-SHA256 600,000 iterations, PBKDF2-HMAC-SHA512 220,000.
  - URL: https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html ; https://api.github.com/repos/OWASP/CheatSheetSeries/commits?path=cheatsheets/Password_Storage_Cheat_Sheet.md&per_page=1
  - Quote: "Use Argon2id with a minimum configuration of 19 MiB of memory, an iteration count of 2, and 1 degree of parallelism."
  - Quote: "PBKDF2-HMAC-SHA256: 600,000 iterations (recommended)"
  - Status: VERIFIED

- Reasoning: OWASP's numbers target server-side password storage where the server pays the cost. Here the client pays it once per encrypt/decrypt and the passphrase is a second factor layered on a 256-bit link key, so the attacker needs the ciphertext, the link key and then a passphrase guess. The OWASP minimum (19 MiB, t=2) is achievable in WASM in well under a second on phones (see Q4 benchmarks) and is a defensible default; 600k PBKDF2-SHA256 is the fallback when Argon2 is unavailable.

## 4. Libraries

### (a) libsodium.js / libsodium-wrappers

- Claim: npm `libsodium-wrappers` latest is 0.8.4, published 2026-04-19, ISC, one runtime dependency (`libsodium`); `libsodium-wrappers-sumo` 0.8.4 the same day.
  - URL: https://registry.npmjs.org/libsodium-wrappers ; https://registry.npmjs.org/libsodium-wrappers-sumo
  - Quote: `latest=0.8.4 published=2026-04-19T11:26:26.616Z deps=libsodium`
  - Status: VERIFIED (registry JSON)

- Claim: Bundle size (minified, gzipped, JS + WASM) about 290 KB standard, 375 KB sumo. The README makes no audit statement and names no libsodium version.
  - URL: https://github.com/jedisct1/libsodium.js
  - Quote: "The standard version weighs about 290 KB and the sumo version about 375 KB"
  - Status: VERIFIED

- Claim: The libsodium audit was sponsored by Private Internet Access and performed by Dr. Matthew Green (Cryptography Engineering), not Cure53, on libsodium v1.0.12 and v1.0.13; the article URL is dated 2017/08 and the page shows "Updated on Jul 22, 2021". Current libsodium is 1.0.22-stable.
  - URL: https://www.privateinternetaccess.com/blog/2017/08/libsodium-audit-results/ ; https://libsodium.gitbook.io/doc
  - Quote: "We are pleased to report that our review did not uncover any critical flaws or vulnerabilities in the core library."
  - Quote (docs): "Private Internet Access, who sponsored a complete security audit"; "JavaScript and WebAssembly versions are also available and fully supported."
  - Status: VERIFIED

- Claim: The JS wrapper exposes secretstream.
  - URL: https://raw.githubusercontent.com/jedisct1/libsodium.js/master/wrapper/symbols/crypto_secretstream_xchacha20poly1305_push.json
  - Quote: `"name": "crypto_secretstream_xchacha20poly1305_push"` with inputs state_address, message_chunk, ad, tag
  - Status: VERIFIED

### (b) @noble/ciphers and @noble/hashes

- Claim: `@noble/ciphers` 2.4.0 and `@noble/hashes` 2.4.0, both published 2026-08-27, MIT, zero runtime dependencies. v2 requires Node 20.19+ and is ESM only.
  - URL: https://registry.npmjs.org/@noble%2Fciphers ; https://registry.npmjs.org/@noble%2Fhashes ; https://github.com/paulmillr/noble-ciphers
  - Quote: `latest=2.4.0 published=2026-08-27T13:07:24.802Z deps= ndeps=0`; README: "For this package, there are 0 dependencies; and a few dev dependencies:"
  - Status: VERIFIED

- Claim: noble-ciphers includes XChaCha20-Poly1305, ChaCha20-Poly1305, AES-GCM, AES-GCM-SIV, AES-SIV and a Web Crypto wrapper for AES-GCM at `@noble/ciphers/webcrypto.js`. Cure53 audited v1.0.0 in September 2024, scope everything, funded by OpenSats; AI-assisted self audits since April 2026.
  - URL: https://github.com/paulmillr/noble-ciphers
  - Quote: "at version 1.0.0, in Sep 2024, independently, by cure53"; "Scope: everything."; "A thin wrapper over built-in `crypto.subtle`, mirroring the noble-ciphers API."; "Minimal: 3KB (gzipped) ChaCha"
  - Report: audit/2024-09-cure53-audit-nbl4.pdf in the repo
  - Status: VERIFIED

- Claim: noble-hashes includes argon2id, scrypt, pbkdf2, hkdf, sha2, sha3, blake3. Cure53 audited v1.0.0 in January 2022 but argon2 was outside scope and remains unaudited. The README advises against Argon2 in pure JS.
  - URL: https://raw.githubusercontent.com/paulmillr/noble-hashes/main/README.md
  - Quote: "at version 1.0.0, in Jan 2022, independently, by Cure53"; "Scope: everything, besides `blake3`, `sha3-addons`, `sha1` and `argon2`, which have not been audited."
  - Quote: "Argon2 can't be fast in JS, because there is no fast Uint64Array."; "It is suggested to use Scrypt instead."
  - Status: VERIFIED

- Claim: noble-hashes benchmarks (Apple M4): argon2id t=1, m=128 MB takes 276 ms; scrypt N=2^19 takes 751 ms; pbkdf2-sha256 2^18 iterations takes 199 ms.
  - URL: same README
  - Quote: "argon2id(t: 1, m: 128MB) x 3 ops/sec @ 276 ms/op"
  - Status: VERIFIED

### (c) hash-wasm

- Claim: `hash-wasm` latest 4.12.0, published 2024-11-19, MIT, zero dependencies (almost two years without a release as of research date).
  - URL: https://registry.npmjs.org/hash-wasm
  - Quote: `latest=4.12.0 published=2024-11-19T19:01:58.186Z deps= ndeps=0`
  - Status: VERIFIED

- Claim: hash-wasm provides Argon2d/i/id (v1.3), PBKDF2 and scrypt (no HKDF), ships WASM as base64 strings, Argon2 bundle 11 kB gzipped, and makes no audit statement.
  - URL: https://github.com/Daninet/hash-wasm
  - Quote: "Argon2: Argon2d, Argon2i, Argon2id (v1.3)"; "WASM modules are bundled as base64 strings (no problems with linking)"; "Zero dependencies"
  - Status: VERIFIED (features); NOT FOUND for any audit (README has none)

### (d) Browser Argon2 performance numbers

- Claim: hash-wasm README benchmark, Chrome v131 on a Ryzen 9 7900X: Argon2id (m=512 KiB, t=8, p=1) 438 ops/s for hash-wasm 4.10.0 vs 213 for argon2-browser 1.18.0 and 195 for argon2-wasm 0.9.0; PBKDF2-SHA512 1000 iterations: hash-wasm 588 ops/s vs noble-hashes 1.3.2 395 ops/s. Note the Argon2 benchmark uses 512 KiB memory, far below OWASP's 19 MiB, so it shows relative speed only.
  - URL: https://github.com/Daninet/hash-wasm
  - Quote: "Chrome v131 on a Ryzen 9 7900X desktop CPU"
  - Status: VERIFIED
- No project publishes a mobile-browser Argon2 timing at OWASP parameters. Status: NOT FOUND (checked the three READMEs above).

## 5. Post-quantum

- Claim: FIPS 203 (ML-KEM), FIPS 204 (ML-DSA) and FIPS 205 (SLH-DSA) are Final, published August 13, 2024. FIPS 203 has a planning note (11/17/2025) that an identified issue will be corrected in a future update.
  - URL: https://csrc.nist.gov/pubs/fips/203/final ; https://csrc.nist.gov/pubs/fips/204/final ; https://csrc.nist.gov/pubs/fips/205/final
  - Quote: "FIPS 205 (Final)" under "08/13/24"
  - Status: VERIFIED

- Claim: NIST's PQC FAQ says AES at all key sizes remains usable and Grover offers little practical advantage.
  - URL: https://csrc.nist.gov/projects/post-quantum-cryptography/faqs
  - Quote: "AES 128 will remain secure for decades to come."; "both AES 192 and AES 256 will still be safe for a very long time."; "current applications can continue to use AES with key sizes 128, 192, or 256 bits."
  - Status: VERIFIED

- Claim: NIST IR 8547 is still an initial public draft (November 12, 2024; comments closed January 10, 2025; no final version, /final returns 404). It deprecates 112-bit quantum-vulnerable public-key algorithms after 2030 and disallows all quantum-vulnerable public-key algorithms after 2035; symmetric primitives with at least 128-bit classical security are rated at least Category 1 (AES-256 is Category 5).
  - URL: https://csrc.nist.gov/pubs/ir/8547/ipd ; https://nvlpubs.nist.gov/nistpubs/ir/2024/NIST.IR.8547.ipd.pdf
  - Quote (Table 3): "112 bits of security strength Deprecated after 2030 Disallowed after 2035"; "≥ 128 bits of security strength Disallowed after 2035"
  - Quote (4.1.3): "all NIST-approved symmetric primitives that provide at least 128 bits of classical security are believed to meet the requirements of at least Category 1"
  - Status: VERIFIED (PDF text extracted)

- Claim: Chrome switched from Kyber to ML-KEM hybrid (codepoint 0x11EC) in Chrome 131 and dropped Kyber (0x6399).
  - URL: https://security.googleblog.com/2024/09/a-new-path-for-kyber-on-web.html (post dated September 13, 2024)
  - Quote: "Chrome will no longer support hybrid Kyber (codepoint 0x6399)."; change lands "in Chrome 131."
  - Status: VERIFIED

- Claim: Firefox 132 (October 29, 2024) added mlkem768x25519 for TLS 1.3 and Mozilla's policy docs say it is on by default.
  - URL: https://www.firefox.com/en-US/firefox/132.0/releasenotes/ ; https://firefox-admin-docs.mozilla.org/reference/policies/postquantumkeyagreementenabled/
  - Quote: "Added support for a post-quantum key exchange mechanism for TLS 1.3 (mlkem768x25519)"; "Firefox offers hybrid X25519 + ML-KEM-768 key agreement by default, so most deployments do not need this policy."
  - Status: VERIFIED

- Claim: Apple ships X25519MLKEM768 in the TLS ClientHello on iOS 26, iPadOS 26, macOS Tahoe 26 and visionOS 26 (the article does not name Safari explicitly; page shows a date of June 18, 2026).
  - URL: https://support.apple.com/en-us/122756
  - Quote: "The ClientHello message from iOS 26, iPadOS 26, macOS Tahoe 26, and visionOS 26 devices will include X25519MLKEM768"
  - Status: VERIFIED

- Claim: Cloudflare has served hybrid PQ key agreement since 2022; over 50% of human traffic was PQ-protected in October 2025 and over 65% by April 2026; PQ certificates are not yet deployed on the public web, and Cloudflare targets 2029 for PQ authentication (ML-DSA to origins mid-2026, Merkle Tree Certificates to visitors mid-2027).
  - URL: https://blog.cloudflare.com/pq-2025/ (October 28, 2025) ; https://blog.cloudflare.com/post-quantum-roadmap/ (April 7, 2026, Bas Westerbaan)
  - Quote (2025): "over 50% (and rising!) of human traffic is protected against store-now/decrypt-later"; "not a single public post-quantum certificate is used"
  - Quote (2026): "over 65% of human traffic to Cloudflare is post-quantum encrypted"; "Mid-2027: PQ authentication support for visitor → Cloudflare connections using Merkle Tree Certificates."
  - Status: VERIFIED

- Reasoning (separate from the facts above): the design has no application-layer asymmetric crypto, so Shor's algorithm has nothing to attack at the application layer. The content keys are 256-bit symmetric keys (NIST Category 5 for AES-256; XChaCha20 likewise 256-bit), which NIST considers safe even under Grover. The transport is TLS 1.3 where all current major browsers and Cloudflare negotiate X25519MLKEM768 by default, so harvest-now-decrypt-later on the wire is already mitigated for most clients. Retention of days further shrinks the window. Application-layer PQ is therefore not needed today. Keep keys at 256 bits rather than 128 (cheap, and it removes the Grover debate entirely), and if a future feature adds recipient public keys, use a hybrid X25519 + ML-KEM-768 construction rather than X25519 alone.

## 6. Randomness and runtimes

- Claim: `crypto.getRandomValues` is specified as cryptographically secure, throws `QuotaExceededError` above 65,536 bytes per call, and MDN prefers `generateKey` for keys only because it is guaranteed to run in a secure context.
  - URL: https://developer.mozilla.org/en-US/docs/Web/API/Crypto/getRandomValues (last modified August 19, 2025) ; https://w3c.github.io/webcrypto/
  - Quote (MDN): "Thrown if the byteLength of typedArray exceeds 65,536."; "Prefer the generateKey() method for key generation, which is guaranteed to be running in a secure context."
  - Quote (spec): "If byteLength is greater than 65536, throw a QuotaExceededError and terminate the algorithm."; "Fill bytes with cryptographically secure random bytes"
  - Status: VERIFIED

- Claim: `crypto.randomUUID()` produces a v4 UUID from a CSPRNG; Baseline since March 2022.
  - URL: https://developer.mozilla.org/en-US/docs/Web/API/Crypto/randomUUID
  - Quote: "generate a v4 UUID using a cryptographically secure random number generator."
  - Status: VERIFIED

- Claim: Node exposes `crypto`, `Crypto`, `CryptoKey` and `SubtleCrypto` as globals (added v17.6.0 / v16.15.0; BCD marks full SubtleCrypto at Node 19.0.0). Node 26 docs list AES-GCM, HKDF, PBKDF2, X25519 and Ed25519 as supported, plus ChaCha20-Poly1305, ML-KEM (v24.7.0) and Argon2 (v24.8.0) from the WICG draft at stability 1.1.
  - URL: https://nodejs.org/api/globals.html ; https://nodejs.org/api/webcrypto.html (v26.12.1)
  - Quote: "A browser-compatible implementation of the Web Crypto API."; "Stability: 1.1 - Active development. Node.js provides an implementation of the following features from the Modern Algorithms in the Web Cryptography API WICG proposal."
  - Status: VERIFIED

- Claim: Cloudflare Workers Web Crypto (page last updated Sep 30, 2026) supports AES-GCM/CBC/CTR/KW, RSA, ECDSA, ECDH, Ed25519, X25519, HMAC, SHA-1/256/384/512, MD5 digest (weak), HKDF and PBKDF2 (deriveBits/deriveKey, importKey), plus non-standard `timingSafeEqual`; ML-DSA and ML-KEM-768/1024 behind the `webcrypto_modern_algorithms` flag; no ChaCha20-Poly1305 or Argon2.
  - URL: https://developers.cloudflare.com/workers/runtime-apis/web-crypto/
  - Quote: "Compare two buffers in a way that is resistant to timing attacks."; "Workers does not implement other algorithms from the draft."
  - Status: VERIFIED

- Claim: The Workers docs page does not mention a PBKDF2 iteration cap, but the open-source workerd runtime enforces one: a default maximum of 100,000 iterations (and a scrypt cost cap of 2^20), reported as a `DOMNotSupportedError`.
  - URL: https://raw.githubusercontent.com/cloudflare/workerd/main/src/workerd/api/crypto/pbkdf2.c++ ; https://raw.githubusercontent.com/cloudflare/workerd/main/src/workerd/api/crypto/impl.c++ ; https://raw.githubusercontent.com/cloudflare/workerd/main/src/workerd/io/limit-enforcer.h
  - Quote: `static constexpr size_t DEFAULT_MAX_PBKDF2_ITERATIONS = 100'000;`; "By default, historically we've limited this to 100,000 iterations max."; "this is an intentional non-conformity."
  - Status: VERIFIED (source code); the docs page itself: PARTIAL

## 7. Key-in-URL-fragment designs

- Claim: The URI spec says the fragment is handled only by the user agent and is separated before dereferencing (so it is not part of the HTTP request).
  - URL: https://www.rfc-editor.org/rfc/rfc3986.html (Sec. 3.5)
  - Quote: "the identifying information within the fragment itself is dereferenced solely by the user agent, regardless of the URI scheme."
  - Status: VERIFIED (the RFC does not literally say "not sent to the server")

- Claim: Referrer Policy strips the fragment before a URL is used as a Referer, so the key does not leak through outbound links or fetches.
  - URL: https://w3c.github.io/webappsec-referrer-policy/ (Editor's Draft, 20 March 2026, Sec. 8.4)
  - Quote: "Set url's fragment to null."
  - Status: VERIFIED

- Claim: Firefox Send (archived) generated a random secret, put it in the URL fragment, derived three keys with HKDF-SHA256 (file keys for ECE/RFC 8188 AES-128-GCM, metadata key, HMAC signing key), uploaded only ciphertext and the signing key, and used PBKDF2 over the password plus the full share URL for the password feature. Record size was 64 KiB.
  - URL: https://github.com/mozilla/send/blob/master/docs/encryption.md ; https://raw.githubusercontent.com/mozilla/send/master/app/ece.js
  - Quote: "The secret key is appended to the share url as a" fragment; "A new signing key is generated via PBKDF2 from the user entered password and the full share url"; `export const ECE_RECORD_SIZE = 1024 * 64;`
  - Status: VERIFIED

- Claim: PrivateBin uses a 32-byte random key, base58 in the fragment, PBKDF2-HMAC-SHA256 with 100,000 iterations and an 8-byte salt, AES-GCM with a 128-bit tag and 16-byte IV; the optional password is mixed into the KDF input. Its FAQ names URL shorteners' access logs as a leak vector.
  - URL: https://github.com/PrivateBin/PrivateBin/wiki/Encryption-format ; https://github.com/PrivateBin/PrivateBin/wiki/FAQ
  - Quote: "These bytes encoded in base58 form the fragment part (characters after `#`) of paste url."; "kdf_iterations = 100000 # was 10000 before PrivateBin version 1.3."
  - Quote (FAQ): "when you submit your URL including the key, it can be retrieved from the shorteners access logs."
  - Status: VERIFIED

- Claim: Bitwarden Send generates a 128-bit secret, derives a 512-bit key with HKDF-SHA256, keeps the key in the fragment, and treats the password purely as server-side authentication, not as a KDF input.
  - URL: https://bitwarden.com/help/send-encryption/
  - Quote: "A new 128-bit secret key is generated for the Send."; "The anchor/fragment/hash is not sent to the server."; "Any password used to protect a Send is not involved in the encryption and decryption of a Send."
  - Status: VERIFIED

- Claim: MEGA security whitepaper.
  - Tried: WebSearch restricted to mega.io/mega.nz (crawler blocked) and WebFetch of https://mega.nz/SecurityWhitepaper.pdf (refused).
  - Status: NOT FOUND

- Claim: A documented, real-world fragment leak through analytics: Element Call (GHSA-6vhh-4xw6-h2h2, CVE-2026-48007, published June 11, 2026, CVSS 4.0 8.6) sent full URLs including the fragment to PostHog, exposing call encryption passwords. Affected `@element-hq/element-call-embedded` 0.5.17 to 0.19.3, fixed in 0.19.4.
  - URL: https://api.github.com/advisories?cve_id=CVE-2026-48007
  - Quote: "were found to contain the full URL of the user's visited page, including the fragment."; "including encryption passwords, to the configured PostHog server."
  - Status: VERIFIED

- Claim: Google Tag Manager's URL variable can be configured to read the fragment of `document.location`, so a tag-manager setup on the page can capture a fragment key without any custom code.
  - URL: https://support.google.com/tagmanager/answer/7683362 ; https://support.google.com/tagmanager/answer/7182738
  - Quote: component options "Full URL, Protocol, Hostname, Port, Path, Filename Extension, Query, and Fragment"; built-in "Page URL" "Provides the full URL of the current page."
  - Status: VERIFIED

- Claim: Browser extensions with the `tabs` permission or host permissions can read `Tab.url`; whether that string includes the fragment is not stated in the Chrome docs (content scripts can read `location.hash` directly in any case).
  - URL: https://developer.chrome.com/docs/extensions/reference/api/tabs
  - Quote: "The last committed URL of the main frame of the tab."; "only present if the extension has the "tabs" permission or has host permissions for the page."
  - Status: PARTIAL

- Claim: Browser history sync storing fragments.
  - Tried: one web search for fragment leaks via history sync; no vendor documentation found.
  - Status: NOT FOUND

## 8. Single-shot AEAD of 5 MB vs chunking

- Claim: A documented Chromium size limit or memory bug for `subtle.encrypt` on multi-megabyte inputs.
  - Tried: web search for Chromium bug tracker entries; only unrelated BoringSSL/NSS results.
  - Status: NOT FOUND

- Claim: The only recent documented large-input problem is in Node, not browsers: inputs that are a multiple of 2 GiB crash the process (CVE-2026-48933, GHSA-pvpp-9x2p-fxg3, published June 26, 2026, affects Node 22/24/26, CVSS 7.5).
  - URL: https://api.github.com/advisories?cve_id=CVE-2026-48933
  - Quote: "A flaw in Node.js WebCrypto implementation can crash the process if the input of `subtle.encrypt()` is a multiple of 2GiB."
  - Status: VERIFIED

- Reasoning: 5 MB is about 2^-13 of the per-invocation AES-GCM limit (2^39 − 256 bits, Q2), so a single-shot `encrypt` is cryptographically fine and costs roughly two to three 5 MB ArrayBuffers in memory. The reasons to chunk are operational: streaming upload/download with progress, bounded memory if the limit is ever raised, and (with secretstream) detection of truncation and reordering per chunk rather than only at the end.

- Claim: RFC 8188 (June 2017, Standards Track) defines chunked AES-128-GCM with a per-record nonce derived from a 96-bit sequence number, a 32-bit record size field, and a minimum record size of 18; the 4096 figure is only an example. Firefox Send used 64 KiB records.
  - URL: https://www.rfc-editor.org/rfc/rfc8188.html
  - Quote: "NONCE = HMAC-SHA-256(PRK, nonce_info || 0x01) XOR SEQ"; SEQ is "a 96-bit unsigned integer in network byte order that starts at zero."; "This uses a record size of 4096 octets and no padding."
  - Status: VERIFIED

- Claim: libsodium's `crypto_secretstream_xchacha20poly1305` is the ready-made chunked construction: it detects truncation, removal, reordering and duplication, rekeys automatically, has no practical stream length limit, and is exposed in libsodium-wrappers (symbol verified in Q4a).
  - URL: https://doc.libsodium.org/secret-key_cryptography/secretstream
  - Quote: "Messages cannot be truncated, removed, reordered, duplicated or modified"; "There are no practical limits to the total length of the stream, or to the total number of individual messages."; "Rekeying happens automatically and transparently, before the internal counter of the underlying cipher wraps."
  - Status: VERIFIED

- Claim: @noble/ciphers and hash-wasm offer no secretstream equivalent; their READMEs list only one-shot AEADs (noble) and hashes/KDFs (hash-wasm).
  - URL: https://github.com/paulmillr/noble-ciphers ; https://github.com/Daninet/hash-wasm
  - Status: PARTIAL (absence inferred from the algorithm lists on the pages)

## 9. Encoding the key in the URL

- Claim: RFC 4648 §5 base64url replaces `+` and `/` with `-` and `_`; padding `=` is normally percent-encoded in URIs and may be omitted when the length is known.
  - URL: https://www.rfc-editor.org/rfc/rfc4648.html (October 2006)
  - Quote: Table 2 "62 - (minus)" and "63 _ (underline)"; "The pad character "=" is typically percent-encoded when used in an URI"; "but if the data length is known implicitly, this can be avoided by skipping the padding."
  - Status: VERIFIED

- Arithmetic (not from a page): 128-bit key = 16 bytes = 22 base64url characters unpadded (24 padded); 256-bit key = 32 bytes = 43 characters unpadded (44 padded). Fixed key length makes padding unnecessary. PrivateBin's base58 of 32 bytes is 43 or 44 characters, so the two encodings are the same size for this purpose.

- Claim: URL length limits in consumer messaging apps.
  - Tried: one web search; only API documentation surfaced in snippets (WhatsApp Business API 4096 characters per text message, Twilio 1600 characters per SMS), none opened.
  - Status: NOT FOUND

## Decision-relevant summary

- AES-256-GCM, HKDF, PBKDF2 and ECDH/X25519 via `crypto.subtle` are available in every current Chrome, Firefox and Safari (desktop and iOS). X25519 needs Chrome 133+, Firefox 130+, Safari 17+. ChaCha20-Poly1305 and Argon2 are not in any browser's Web Crypto and only exist in a WICG draft, so XChaCha20 or Argon2 in the browser means a library (WASM or JS).
- A fresh random 256-bit content key per message makes the AES-GCM 2^32-invocation and 2^-32 collision rules irrelevant; use 96-bit IVs (random or counter) per chunk and never reuse a content key across messages. 5 MB is far below the 2^39 − 256-bit per-invocation limit.
- NIST will revise SP 800-38D (short tags removed, IV guidance clarified) but no draft exists; nothing planned affects 96-bit IV + 128-bit tag.
- Passphrase KDF: Argon2id at OWASP's minimum (19 MiB, t=2, p=1) or RFC 9106's second option (64 MiB, t=3, p=4). PBKDF2-HMAC-SHA256 at 600,000 iterations is the Web Crypto-only fallback. Note that Cloudflare Workers cap PBKDF2 at 100,000 iterations, so do not run the KDF server side on Workers.
- Argon2 library choice: hash-wasm (WASM, fastest, zero deps, no audit, last release November 2024) or @noble/hashes (JS, zero deps, Cure53 audited except argon2, author recommends scrypt instead). Neither has an audited Argon2. libsodium-wrappers has Argon2id and secretstream in one audited C codebase but weighs about 290 KB gzipped.
- AEAD library choice: @noble/ciphers 2.4.0 (Cure53, September 2024, whole scope, zero deps, ESM-only, Node 20.19+) gives XChaCha20-Poly1305 and a thin AES-GCM wrapper over Web Crypto. libsodium.js has the only secretstream API.
- Audit facts to cite correctly: libsodium was audited by Matthew Green / Cryptography Engineering (sponsored by PIA, 2017, v1.0.12/1.0.13), not Cure53; noble-ciphers by Cure53 in September 2024; noble-hashes by Cure53 in January 2022 with argon2 excluded.
- Post-quantum: FIPS 203/204/205 are final (August 2024); IR 8547 is still a draft but its 2030/2035 dates target public-key algorithms only. NIST says AES-128 through AES-256 stay safe. Chrome 131+, Firefox 132+, iOS/macOS 26 and Cloudflare all negotiate X25519MLKEM768 in TLS. With no application-layer asymmetric crypto and days of retention, no application-layer PQ is needed; keep 256-bit keys.
- Fragment leak vectors with documented evidence: analytics/tag managers reading `document.location` (Element Call CVE-2026-48007; GTM URL variable "Fragment" component) and URL shorteners (PrivateBin FAQ). Referer and HTTP requests do not carry the fragment (RFC 3986, Referrer Policy). Ship the viewer page with no third-party scripts, and consider PrivateBin's dash-prefixed fragment or a click-to-reveal step so link previews and prefetchers do not burn one-time links.
- Prior art converges on: random key in fragment, HKDF to split it into sub-keys (Firefox Send, Bitwarden), AES-GCM, 64 KiB records when chunking (Firefox Send). Bitwarden uses the password as auth only; Firefox Send mixed password and full URL into PBKDF2; PrivateBin mixes the password into the KDF.
- `crypto.getRandomValues` is limited to 65,536 bytes per call (fine for keys and IVs); `crypto` is a global in Node 19+ and in Workers, so one TypeScript crypto module can target browser, Node CLI and Workers if it sticks to AES-GCM/HKDF/PBKDF2 or a pure JS/WASM library.
- Key encoding: base64url unpadded, 43 characters for 256 bits; consumer messaging URL limits are not documented in any source found.

## URLs opened

- https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto
- https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/deriveKey
- https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/encrypt
- https://raw.githubusercontent.com/mdn/browser-compat-data/main/api/SubtleCrypto.json
- https://raw.githubusercontent.com/mdn/browser-compat-data/main/package.json
- https://caniuse.com/cryptography
- https://github.com/WICG/webcrypto-modern-algos
- https://wicg.github.io/webcrypto-modern-algos/
- https://api.github.com/repos/WICG/webcrypto-modern-algos/commits?per_page=1
- https://developer.chrome.com/release-notes/133
- https://developer.chrome.com/release-notes/137
- https://developer.chrome.com/release-notes/155 (404)
- https://www.firefox.com/en-US/firefox/130.0/releasenotes/
- https://www.firefox.com/en-US/firefox/132.0/releasenotes/
- https://webkit.org/blog/14445/webkit-features-in-safari-17-0/
- https://developer.apple.com/documentation/safari-release-notes/safari-17-release-notes (body not retrievable)
- https://w3c.github.io/webcrypto/
- https://csrc.nist.gov/pubs/sp/800/38/d/final
- https://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf
- https://csrc.nist.gov/News/2024/nist-to-revise-sp-80038d-gcm-and-gmac-modes
- https://csrc.nist.gov/pubs/sp/800/38/d/r1/ipd (404)
- https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-aead-limits
- https://www.rfc-editor.org/rfc/rfc9106.html
- https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html
- https://api.github.com/repos/OWASP/CheatSheetSeries/commits?path=cheatsheets/Password_Storage_Cheat_Sheet.md&per_page=1
- https://registry.npmjs.org/libsodium-wrappers
- https://registry.npmjs.org/libsodium-wrappers-sumo
- https://registry.npmjs.org/@noble%2Fciphers
- https://registry.npmjs.org/@noble%2Fhashes
- https://registry.npmjs.org/hash-wasm
- https://github.com/jedisct1/libsodium.js
- https://raw.githubusercontent.com/jedisct1/libsodium.js/master/wrapper/symbols/crypto_secretstream_xchacha20poly1305_push.json
- https://github.com/jedisct1/libsodium
- https://libsodium.gitbook.io/doc (redirect target of https://doc.libsodium.org/)
- https://www.privateinternetaccess.com/blog/libsodium-audit-results/
- https://www.privateinternetaccess.com/blog/2017/08/libsodium-audit-results/
- https://github.com/paulmillr/noble-ciphers
- https://github.com/paulmillr/noble-hashes
- https://raw.githubusercontent.com/paulmillr/noble-hashes/main/README.md
- https://github.com/Daninet/hash-wasm
- https://csrc.nist.gov/pubs/fips/203/final
- https://csrc.nist.gov/pubs/fips/204/final
- https://csrc.nist.gov/pubs/fips/205/final
- https://csrc.nist.gov/projects/post-quantum-cryptography/faqs
- https://csrc.nist.gov/pubs/ir/8547/ipd
- https://csrc.nist.gov/pubs/ir/8547/final (404)
- https://nvlpubs.nist.gov/nistpubs/ir/2024/NIST.IR.8547.ipd.pdf
- https://security.googleblog.com/2024/09/a-new-path-for-kyber-on-web.html (and ?m=1)
- https://firefox-admin-docs.mozilla.org/reference/policies/postquantumkeyagreementenabled/
- https://support.apple.com/en-us/122756
- https://blog.cloudflare.com/pq-2025/
- https://blog.cloudflare.com/post-quantum-roadmap/
- https://developer.mozilla.org/en-US/docs/Web/API/Crypto/getRandomValues
- https://developer.mozilla.org/en-US/docs/Web/API/Crypto/randomUUID
- https://nodejs.org/api/globals.html
- https://nodejs.org/api/webcrypto.html
- https://developers.cloudflare.com/workers/runtime-apis/web-crypto/
- https://raw.githubusercontent.com/cloudflare/workerd/main/src/workerd/api/crypto/pbkdf2.c++
- https://raw.githubusercontent.com/cloudflare/workerd/main/src/workerd/api/crypto/impl.c++
- https://raw.githubusercontent.com/cloudflare/workerd/main/src/workerd/io/limit-enforcer.h
- https://raw.githubusercontent.com/cloudflare/workerd/main/src/workerd/api/crypto/kdf.c++ (404)
- https://www.rfc-editor.org/rfc/rfc3986.html
- https://w3c.github.io/webappsec-referrer-policy/
- https://github.com/mozilla/send/blob/master/docs/encryption.md
- https://raw.githubusercontent.com/mozilla/send/master/app/ece.js
- https://github.com/PrivateBin/PrivateBin/wiki/Encryption-format
- https://github.com/PrivateBin/PrivateBin/wiki/FAQ
- https://bitwarden.com/help/send-encryption/
- https://mega.nz/SecurityWhitepaper.pdf (blocked)
- https://api.github.com/advisories?cve_id=CVE-2026-48007
- https://api.github.com/advisories?cve_id=CVE-2026-48933
- https://support.google.com/tagmanager/answer/7182738
- https://support.google.com/tagmanager/answer/7683362
- https://developer.chrome.com/docs/extensions/reference/api/tabs
- https://doc.libsodium.org/secret-key_cryptography/secretstream
- https://www.rfc-editor.org/rfc/rfc8188.html
- https://www.rfc-editor.org/rfc/rfc4648.html
