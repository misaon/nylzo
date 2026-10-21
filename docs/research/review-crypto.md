# Crypto review: Nylzo protocol v1

Independent adversarial review. Research date 2026-10-21. Inputs: design document (sections "Model hrozeb", "B1", "Protokol v1") and appendices A, D, E, H. Browser and library availability claims were re-checked today; statuses are listed in section (e). Where I rely on an appendix fact without reopening it, I say so.

## (a) Verdict

The claim holds for the symmetric core, with two conditions: AES-256-GCM, HKDF-SHA-256, PBKDF2 and the OS CSPRNG are the strongest primitives that exist natively and cross-browser in Web Crypto today (Chrome 156, Firefox 158, Safari 27), and every "more modern" option (Argon2id, ChaCha20-Poly1305, AES-GCM-SIV, ML-KEM) is either absent from all three browsers, Chrome-only behind an experiment, or reachable only through libraries whose audit is older or narrower than the native code they would replace. Condition one: the construction has one real downgrade hole, because the recipient takes kdf_id from the server and a malicious server can therefore obtain a PBKDF2-cost verifier for a scrypt-protected message. Condition two: a handful of hygiene gaps (salt from the public id, unpadded files, unlabeled pepper HMAC, understated TLS-edge oracle) should be closed before the word "safest" goes into documentation; none of the surveyed alternatives would raise security in a one-key-per-message design, and most would lower auditability.

## (b) Primitive table

| Primitive / choice | Current design | Strongest alternative actually available (Oct 2026) | Rec. | Reason |
|---|---|---|---|---|
| AEAD | AES-256-GCM, 128-bit tag, Web Crypto and Go crypto/cipher | ChaCha20-Poly1305: Chrome origin trial M151-154, chromestatus "Proposed", Firefox and Safari none. AES-GCM-SIV: no browser, not in the WICG draft, not in Go stdlib or x/crypto. XChaCha20-Poly1305: @noble/ciphers only (pure JS) | Keep | Native, hardware AES, inside the FIPS module. One key per message makes nonce-misuse resistance and 192-bit nonces worthless here |
| Key sizes | S 256, K/R/V/M/L 256, id 128 bits | none | Keep | NIST Category 5; Grover is moot |
| IV | 96-bit random per message, single shot | IV derived from HKDF | Keep random | Protects against accidental K reuse when a client retries with the same id and S; collision budget is unreachable at one message per key |
| HKDF hash | SHA-256, salt = id, info labels | HKDF-SHA-512 (Web Crypto and Go support it) | Keep | No gain at 256-bit outputs; SHA-256 is everywhere and in the FIPS module |
| S ‖ P into Extract | S (32 B) ‖ P (32 B or empty) | none | Keep | Fixed lengths make the concatenation unambiguous; Extract is HMAC, so no length extension |
| Passphrase salt | SHA-256("nylzo/v1/pw-salt" ‖ id), public | HKDF-Expand(PRK_S, "nylzo/v1/pw-salt", 32), i.e. from S | Switch (cheap) | Stops per-target precomputation by anyone who sees the id without S (F2) |
| Passphrase KDF | scrypt N=2^17 r=8 p=1 (noble-hashes, x/crypto); PBKDF2-SHA256 600k as profile 2 | Argon2id: no browser Web Crypto; not even in Chrome's experiment list; no Mozilla or Apple interest per Igalia notes. Libraries: hash-wasm (no audit, last release 11/2024), noble argon2 (unaudited, author says use scrypt), libsodium.js (290 KB, 2017 audit of C code) | Keep scrypt; fix profile selection (F1) | scrypt at 128 MiB is memory-hard and audited; OWASP: use scrypt "when the former is not available" |
| Verifier V | HKDF-Expand(PRK_SP, "verifier"); server stores HMAC(pepper, SHA-256(V)) | A PAKE (OPAQUE) would keep V off the wire; no Web Crypto support, only unaudited WASM | Keep; document the oracle (F3) | V or h(V) plus S is an offline passphrase oracle; the pepper does not change that |
| Pepper HMAC | HMAC-SHA-256(pepper, x), one key for h(R), M, L and the IP hash | Per-purpose keys from HKDF(pepper, label) | Switch (cheap) | Domain separation and a rotation path (F5) |
| AAD | header ‖ id ‖ policy | Add instance origin (optional) | Keep | Binds version, kdf_id, flags, IV, id and policy to the ciphertext |
| Padding | Text-only to 4 KiB; files unpadded | Pad every plaintext to 4 KiB | Switch | File length leaks to the byte (F4) |
| URL encoding | base64url unpadded, `1.` prefix | base58 (PrivateBin) | Keep | Same length; the version prefix is right |
| Randomness | crypto.getRandomValues, Go crypto/rand | none | Keep | Both are the OS CSPRNG |
| Asymmetric (v2) | None; HPKE reserved | X25519/Ed25519 native since Chrome 133/137, Firefox 130/129, Safari 17. ML-KEM in Web Crypto: BCD says Chrome 155 (experimental), Firefox Nightly, Safari none; chromestatus still "Proposed". Go 1.26 crypto/hpke ships X-Wing | n/a for v1 | A hybrid in the browser needs a library for the foreseeable future |

## (c) Findings

**F1 (Medium) KDF-profile downgrade through server-supplied kdf_id.** The recipient must compute V before the claim and learns kdf_id from `GET /messages/{id}` (later also from `X-Nylzo-Policy`), both server-controlled. A malicious server, or a TLS-terminating edge that rewrites responses, returns kdf_id = 2 for a scrypt message. The client computes V' = HKDF(S ‖ PBKDF2(pass)) and sends it. The server now holds a PBKDF2-cost verifier for the real passphrase; with S from any link leak (T9, T12) it grinds at a cost lower by an order of magnitude or more on GPU hardware than the scrypt guarantee the design promises. AAD binding does not help: V' is produced before any decryption. Fix: carry kdf_id in the fragment (for example `#1.{kdf}{S}` or a second dotted field) and have the client ignore the server's value for derivation; or drop profile 2 entirely. Also define who chooses profile 2 (the sender's device, only when scrypt allocation fails) and state that the server never chooses.

**F2 (Low) Public-id salt allows per-target precomputation.** salt_pw depends only on id, which sits in the URL path, server logs and tombstones. Anyone who sees an id can run the expensive scrypt step for candidate passphrases before holding S, the ciphertext or the verifier, then finish instantly when any of those leaks. Total work is unchanged; it is moved in time. Fix: salt_pw = HKDF-Expand(PRK_S, "nylzo/v1/pw-salt", 32). S is in the link at both create and claim time, so the "salt must exist before the envelope" constraint still holds, and the id stays bound through PRK_S.

**F3 (Low) T13 and T14 understate the TLS edge.** The edge sees h(V) at creation and R, V and L at claim. h(V) or V plus S gives offline passphrase guessing with no pepper, no ciphertext and no burned open; the design text says the pepper is required for that. Fix: rewrite T13 ("holds an offline passphrase oracle from the moment the link leaks") and add the T13 + link-leak combination to T14; for self-hosting recommend terminating TLS in the Go process or on the same host; never log the `X-Nylzo-Create` header or claim bodies.

**F4 (Low) Files are not padded.** A message with a file reveals the file length to the byte (object length minus 15, 16, 4 and the meta JSON). Fix: pad every plaintext to a 4 KiB multiple (at most 4 KiB on 5 MiB) and drop flags bit 1, which also exposes "text only" in the clear header.

**F5 (Low) One pepper key, no purpose labels, no rotation path.** mac(h(R)), mac(M), mac(L) and the IP hash all use HMAC(pepper, ·) over unlabeled inputs; date ‖ IP strings and 32-byte tokens share one input space. No practical collision today, but it blocks clean rotation. Fix: k_purpose = HKDF(pepper, "nylzo/v1/mac/" + purpose); store a pepper_id per row so rotation can be added without a format change.

**F6 (Low) The post-quantum transport claim depends on the edge.** Browsers offer X25519MLKEM768 and Go 1.26+ enables hybrids by default when it terminates TLS, but nothing in the appendices shows Scaleway's load balancer negotiating it (NOT FOUND). Fix: test the public instance with `openssl s_client -groups X25519MLKEM768`; if it fails, phrase the threat model as "PQ on the wire when TLS terminates in Go or Caddy".

**F7 (Info) noble-hashes 2.4.0 versus the 2022 audit.** Cure53 audited v1.0.0 in January 2022; v2 is an ESM rewrite and the README now lists only "AI-assisted self-audits" since April 2026. scrypt is small: pin the exact version, enforce lockfile integrity, run RFC 7914 test vectors in CI, and consider vendoring the single module.

**F8 (Info) Create-time collision check is an unauthenticated liveness oracle** for a known id (201 versus conflict). Harmless at 128 bits; document it, and include tombstones in the check.

**F9 (Info) Derive K as a non-extractable CryptoKey** (deriveKey, extractable = false); use deriveBits only for R and V. Prevents accidental export by future code at no cost.

**F10 (Info) HKDF-Extract salt is 16 bytes, below HashLen.** RFC 5869 allows it; SHA-256("nylzo/v1/salt" ‖ id) as a 32-byte salt is cosmetic.

**F11 (Info, untested) A fragment ending in `-` or `_` may be clipped by some autolinkers.** Regenerating S when the last character is `-` or `_` costs 0.05 bits.

**F12 (Info, v2) Safari's Web Crypto Ed25519 signatures are randomized** (BCD note). Fine for verification, bad for deterministic test vectors; plan v2 tests accordingly.

**F13 (Info) The FIPS 140-3 statement covers AES, HKDF, HMAC and PBKDF2 only;** scrypt lives in x/crypto outside the validated module. Say so wherever GOFIPS140 is mentioned.

On "most modern": Argon2id via WASM, XChaCha20 via JS and the Web Crypto ML-KEM/ChaCha20 experiment would each replace native, audited, hardware-backed code with less-audited code, and none of them adds security to a design that uses every key once. "Latest browsers only" unlocks Trusted Types, CSP nonces, non-extractable keys and native X25519/Ed25519 for v2; it does not unlock anything that would change v1's symmetric core.

## (d) Do not change

AES-256-GCM single shot with a random 96-bit IV; HKDF-SHA-256 with the existing info labels; 256-bit S, K, R, V, M, L and the 128-bit id; S ‖ P into Extract with id as salt (it also binds keys to the id); h(R) and h(V) at creation followed by peppered HMAC (it keeps R and V off the wire until claim); scrypt N=2^17 as profile 1; V as an online verifier with rate limiting; base64url with the `1.` prefix; no application-layer asymmetric crypto in v1; no chunking at 5 MiB. Do not adopt Argon2id, XChaCha20-Poly1305, AES-GCM-SIV, SHA-512 or Web Crypto ML-KEM for v1.

## (e) Sources opened

- https://wicg.github.io/webcrypto-modern-algos/ — "Draft Community Group Report" 14 September 2026; "This is an unofficial proposal."; abstract lists ML-KEM, Hybrid KEMs, ML-DSA, SLH-DSA, AES-OCB, ChaCha20-Poly1305, SHA-3, KMAC, Argon2; no AES-GCM-SIV or XChaCha20 — VERIFIED
- https://chromestatus.com/api/v0/features/5198951632470016 — "Algorithm Updates in WebCrypto"; chrome status "Proposed"; desktop/android milestone 155; origin trial 151-154; updated 2026-09-11 — VERIFIED
- https://groups.google.com/a/chromium.org/g/blink-dev/c/TF8y175J3YU/m/J4thHvVVAAAJ — Intent to Experiment, 17 June 2026; ML-KEM 768/1024, ML-DSA, ChaCha20-Poly1305, X-Wing; "We'd like to get a few sites to test out the new algorithms in real-world scenarios before shipping it" — VERIFIED; an Intent to Ship thread — NOT FOUND (two searches)
- https://developer.chrome.com/release-notes/154 — stable "September 22nd, 2026", no Web Crypto items — VERIFIED; /release-notes/155 and /156, /blog/new-in-chrome-155 — NOT FOUND (404)
- https://chromiumdash.appspot.com/fetch_releases?channel=Stable&platform=Windows&num=2 — 156.0.8078.48, milestone 156, 2026-10-20 — VERIFIED; fetch_milestone_schedule?mstone=155 — stable_date 2026-10-06 — VERIFIED
- https://raw.githubusercontent.com/mdn/browser-compat-data/main/api/SubtleCrypto.json — encapsulateKey chrome 155 / firefox "preview" / safari false, experimental true; X25519 133/130/17; Ed25519 137/129/17; Safari "randomized signatures" note — VERIFIED; commit "Updates for Chrome 155 beta (#30575)" 2026-09-22 — VERIFIED
- https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/encapsulateKey — NOT FOUND (404)
- https://www.firefox.com/en-US/firefox/notes/ — "Version 158.0.1, first offered to Release channel users on October 20, 2026"; no crypto items — VERIFIED
- https://webkit.org/blog/18325/webkit-features-for-safari-27-0/ — 17 September 2026; only "Fixed CryptoKey to correctly remain associated with its secure context." — VERIFIED; STP 254 (8 October 2026) no crypto items — VERIFIED
- https://github.com/Igalia/webengineshackfest/wiki/2026-Web-Cryptography-API — "Anne: CryptoKit are only concerned about Argon2"; "the lack of interest from Mozilla an Safari"; criterion "being widely supported" — VERIFIED
- https://developer.microsoft.com/microsoft-edge/origin-trials/trials/f5dbdb03-9776-4ee2-9560-4d68733c7531 — Edge "Additional WebCrypto Algorithms" trial, expires 20 October 2026 — PARTIAL (search snippet only)
- https://raw.githubusercontent.com/paulmillr/noble-hashes/main/README.md — audit "at version 1.0.0, in Jan 2022, independently, by Cure53", argon2 excluded; "It is suggested to use Scrypt instead."; "AI-assisted self-audits in Apr 2026" — VERIFIED
- https://raw.githubusercontent.com/paulmillr/noble-ciphers/main/README.md — Cure53 Sep 2024 v1.0.0, "Scope: everything"; gcmsiv and xchacha20poly1305 listed — VERIFIED
- https://go.dev/doc/go1.26 — "The new crypto/hpke package implements Hybrid Public Key Encryption (HPKE) as specified in RFC 9180, including support for post-quantum hybrid KEMs." — VERIFIED
- https://go.dev/doc/go1.27 — new crypto/mldsa; "The MLKEM1024 key exchange is now supported." — VERIFIED
- https://pkg.go.dev/crypto/hpke — KEMs X25519, MLKEM768, MLKEM1024, MLKEM768-X25519 (X-Wing); AEADs AES-256-GCM, ChaCha20-Poly1305 — VERIFIED
- https://pkg.go.dev/golang.org/x/crypto — v0.58.0, 9 October 2026; argon2, scrypt, chacha20poly1305 present; no gcmsiv package — VERIFIED
- https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html — "While Argon2id should be the best choice for password hashing, scrypt should be used when the former is not available."; scrypt N=2^17 r=8 p=1; PBKDF2-HMAC-SHA256 600,000 — VERIFIED
- https://developer.mozilla.org/en-US/docs/Web/API/HkdfParams — hash SHA-256/384/512; "salt does not need to be kept secret" — VERIFIED
- Relied on from appendix A without reopening: NIST SP 800-38D IV and 2^32 bounds; RFC 9106; hash-wasm no audit, last release 2024-11-19; libsodium audit by Matthew Green 2017; Chrome 131 / Firefox 132 / Apple OS 26 X25519MLKEM768 in TLS. From appendix D: WebKit bug 269777 (WASM memory). From appendix H: Go FIPS module certificate #5247.
