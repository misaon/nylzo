# OD6: scrypt N=2^17 in Safari

Date: 23 October 2026. Each result carries its status: VERIFIED (measured here), NOT RUN, or PENDING.

## Question

Design OD6: does Safari run passphrase profile 1 (scrypt N=2^17, r=8, p=1, 128 MiB) through `@noble/hashes` 2.4.0 in acceptable time, and does iOS keep the page alive? If iOS does not, the public instance switches its default to profile 2 (PBKDF2-HMAC-SHA-256, 600,000 iterations, kdf_id 2).

## Method

A test page and a small Node server lived in a scratch directory outside the repository. The page loaded the unmodified ES modules of `@noble/hashes` 2.4.0 from `web/core/node_modules` and was served with the HTML CSP of spec §9 (`script-src 'nonce-…'`, `style-src 'nonce-…'`, `require-trusted-types-for 'script'`). The worker variant added `worker-src 'self'` and created the worker URL through a Trusted Types policy.

One run of the page:

- 5 × `scryptAsync` with N=2^17, r=8, p=1, dkLen=32. The first call uses the vector `scrypt-ascii` from `spec/vectors.json` and checks the derived P; the other four use random salts.
- 16 × `scryptAsync` with N=2^14, r=8, p=1, the cost of one proof-of-work evaluation (OD24). These always ran on the main thread.
- 2 × PBKDF2-HMAC-SHA-256 with 600,000 iterations through Web Crypto, when `crypto.subtle` exists.

Durations come from `performance.now()`. During every derivation the page also recorded the longest gap between two animation frames, which shows whether the UI thread stays responsive. Two modes were measured: `main` calls `scryptAsync` on the main thread, as `web/core` does today; `worker` starts a module Worker for each derivation and terminates it once the key is back.

Memory: on macOS a shell loop sampled the RSS of every Safari WebContent process with `ps` about three times per second. RSS is not the phys_footprint figure that iOS uses for its limits, so the numbers show the trend, not the iOS budget. Crash detection: the page sets a localStorage marker before each derivation and clears it afterwards; a page that loads with the marker set reports the run as interrupted.

## Results

### macOS Safari 27.0.1: VERIFIED

MacBook Pro, Apple M4 Pro, 24 GB, macOS 27.0.1. Each mode ran in a fresh tab of a freshly started Safari.

| Measurement | main | worker |
|---|---|---|
| scrypt N=2^17, 5 derivations (ms) | 285, 277, 273, 277, 268 | 411, 410, 399, 389, 401 |
| vector `scrypt-ascii` | matches | matches |
| longest frame gap during a derivation (ms) | 21 to 24 | 11 (28 in the first) |
| WebContent RSS before the run, peak after 5 derivations | 46 MiB, 653 MiB | 60 MiB, 215 MiB |
| RSS after the run | 586 MiB after about 60 s, 447 MiB after about 3 min | 105 MiB after about 70 s, 96 MiB after about 3 min |
| PoW scrypt N=2^14, mean of 16 (ms, main thread) | 32.3 | 33.6 |
| PBKDF2 600,000 (ms) | 84, 84 | 85, 84 |

An earlier pilot run in the main mode gave 309, 278, 271, 267 and 271 ms and a peak of 659 MiB that stayed for more than 90 s. A second pilot of the worker mode ran while Safari had restored the pilot tab in the background, so it is not counted.

What the numbers show:

- In the main mode every derivation allocates a new 128 MiB block (noble needs 128·r·(N+p+1) bytes, 128 MiB plus 2 KiB), and JavaScriptCore frees it only at a later collection. Five derivations, which is how many wrong passphrases a reader may try without waiting (OD13), kept about 600 MiB above the baseline for more than a minute.
- With a worker terminated after each derivation, the peak stays at one derivation, about 155 MiB above the baseline, however many attempts follow. The UI thread has no frame longer than 11 ms after the first derivation. The price is about 125 ms per derivation on the M4 Pro, because each new worker parses and compiles noble again.
- The expected PoW time is 16 evaluations at difficulty 4, about 0.52 s on this machine, and 256 at difficulty 8, about 8.3 s.

### iOS Simulator: NOT RUN

The test Mac has only the Command Line Tools and no full Xcode, so no simulator is available. The simulator would not answer the main question anyway, since it runs on the Mac's memory and applies no iOS memory limits.

### iPhone: PENDING

The memory limit iOS sets for a Safari WebContent process is not documented by Apple (NOT FOUND); a process above it is killed and Safari reloads the page. Whether 650 MiB of uncollected scrypt memory is safe on a given iPhone is therefore open until measured on a device. The owner runs the same page on a real iPhone over the local network, in both modes and with 10 derivations, which covers the 5 attempts without waiting plus margin. Over plain http on the local network the page is not a secure context, so `crypto.subtle` is missing and the PBKDF2 step is skipped; scrypt and workers do not need it.
