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

### iPhone over plain http: VERIFIED for one run, cause of the slowness NOT VERIFIED

The owner's iPhone 15 Pro Max with iOS 27.2 and Safari 27.2 (the user agent reports the frozen `iPhone OS 18_7`; model and iOS version come from the owner), screen 430 × 932 CSS px at device pixel ratio 3, run over plain http on the local network, so not a secure context and without `crypto.subtle`. The page ran in the main mode with 5 derivations on 23 October 2026, 10:44 to 10:49 UTC.

| Measurement | main |
|---|---|
| scrypt N=2^17, 5 derivations (ms) | 43195, 43175, 43021, 45141, 45432 |
| vector `scrypt-ascii` | matches |
| longest frame gap during a derivation (ms) | 27 to 74 |
| page interrupted | no, all 5 derivations finished and the summary arrived |
| PoW scrypt N=2^14, mean of 16 (ms) | 5572 (5491 to 5667) |
| expected PoW time at difficulty 4 and 8 | about 89 s and about 24 min |
| PBKDF2 600,000 | not run, no Web Crypto outside a secure context |
| memory | not measured, Safari on iOS gives the page no memory figure |

One derivation took about 160 times longer than in Safari on the Mac. A gap that large fits JavaScript running without the JIT compiler, which is what Lockdown Mode does in Safari, better than the speed of the phone itself. The owner reports that Lockdown Mode is off on the phone, so that does not explain it, and the cause is NOT VERIFIED. A second candidate is the yield between work slices: `scryptAsync` hands control back through `scheduler.yield()` or `setTimeout(0)` after about 10 ms of work, so a slow yield adds time without any extra computation. The page now reports whether WebAssembly exists and how long a plain loop of 2·10^7 steps takes (16 to 23 ms in Safari on the Mac), and its `sync` mode runs scrypt without yielding (246 and 254 ms in Safari on the Mac). The worker and sync modes have not run on the iPhone yet.

Supporting data point, observed once and not reproduced: the HTTPS probe for the next phone run (see the plan) ran headless in Playwright's WebKit 27.2 on the M4 Pro Mac. In the first run `setTimeout(0)` took 80 ms on average and `scheduler.yield` did not exist; scrypt profile 1 then took 9.5 s with yielding (`scryptAsync`) and 0.9 to 1.2 s without yielding, on the main thread and in a worker alike. A second run in the same setup minutes later measured 6.4 ms per `setTimeout(0)` and 0.3 s with yielding. Headless Chromium showed 4.5 ms per `setTimeout(0)` and no difference between the modes (0.2 to 0.26 s). The yielding scrypt thus depends on how the browser schedules timers at that moment, and the blocking scrypt that the web client now runs in its worker does not. Whether the iPhone throttled timers is still NOT VERIFIED.

Owner decision D30 (23 October 2026): the default passphrase profile is PBKDF2 (profile 2) on the public instance and in the default `nylzod` configuration.

### iPhone over HTTPS: VERIFIED

The same iPhone 15 Pro Max with iOS 27.2 opened the static probe from a temporary Codeberg Pages site over HTTPS on 23 October 2026 at 11:38 UTC, so in a secure context with Web Crypto. The probe ran every mode once and sent nothing; the owner pasted its JSON.

| Measurement | value |
|---|---|
| secure context, Web Crypto, WebAssembly | yes, yes, yes |
| loop of 2·10^7 steps | 28 ms, so the JIT runs |
| `scheduler.yield`, mean `setTimeout(0)` | absent, 6.4 ms |
| PBKDF2 600,000 (ms) | 119, 82 |
| scrypt N=2^17 in a new worker, blocking, as the web client runs it (ms) | 587, 547, 533 |
| scrypt in a new worker, yielding (ms) | 742 |
| scrypt on the main thread, blocking and yielding (ms) | 233 and 340 |
| vector `scrypt-ascii` | matches in every mode |
| PoW scrypt N=2^14 in a worker, mean of 16 (ms) | 51.5, so about 0.8 s at difficulty 4 and about 13 s at difficulty 8 |
| page interrupted | no, 7 derivations of 128 MiB in a row |

Over HTTPS profile 1 takes about 0.55 s on the phone in the web client's path, the extra 0.3 s over the main thread being the start of a new worker. That meets the criterion of the milestone plan (median under 3 s, no interruption). The 43 s of the first run therefore came from that run's conditions, plain http from the local network, and not from the phone's speed; which part of them caused it is NOT VERIFIED. The web client does not run over plain http anyway, because Web Crypto exists only in a secure context.

Owner decision D33 (23 October 2026), after the run over HTTPS: the default passphrase profile is scrypt (profile 1) again, replacing D30. Owner decision D34: OD25 closes without a change to the proof of work.

Owner decision D71 (24 October 2026): Nylzo drops profile 2. The measurements over HTTPS above show that scrypt profile 1 runs on the iPhone in the web client's path, so the condition for the PBKDF2 fallback did not occur. `kdf_id` 2 stays reserved. Only this one phone was measured; older iPhones with less memory were not.
