# Nylzo protocol v1

Status: normative transcription of the approved design (`docs/designs/nylzo-architecture-research.md`, 21 October 2026). This document adds no design decisions. Three wire details that the design leaves open are fixed here and marked **[detail]**: the canonical encoding of `meta`, the content of `X-Nylzo-Policy`, and the proof-of-work token format. Everything else restates the design; where the two disagree, the design wins and this file has a bug.

The key words MUST, MUST NOT, SHOULD and MAY are to be read as in RFC 2119.

Test vectors for every client-side construction live in `spec/vectors.json`. Both reference cores (`internal/core` in Go, `web/core` in TypeScript) MUST pass them.

## 1. Primitives

| Symbol | Definition |
|---|---|
| `h(x)` | SHA-256(x), 32 bytes |
| HKDF | HKDF-SHA-256 (RFC 5869); `Extract(salt, IKM)` and `Expand(PRK, info, L)` |
| AEAD | AES-256-GCM, 96-bit IV, 128-bit tag (NIST SP 800-38D) |
| scrypt | RFC 7914 |
| HMAC | HMAC-SHA-256 (RFC 2104) |
| Ed25519 | EdDSA over edwards25519 (RFC 8032), 32-byte seed, 32-byte public key, 64-byte signature |
| CSPRNG | `crypto.getRandomValues` in browsers, `crypto/rand` in Go |
| `a \|\| b` | byte concatenation |
| `u16be(n)`, `u32be(n)`, `u64be(n)` | unsigned big-endian integers of 2, 4 and 8 bytes |

No other cryptographic construction is used. Ed25519 only signs requests (§5.5); content encryption stays symmetric, and flags bit 0 is reserved for a future HPKE mode.

### 1.1 base64url

All binary values carried in URLs, headers and JSON use base64url (RFC 4648 §5) **without padding** and in **canonical form**: a decoder MUST reject a string that is not the exact encoding of its decoded bytes. Concretely, the unused low bits of the final character MUST be zero, and the length MUST match the expected byte length exactly.

| Value | Bytes | Characters | Allowed final characters |
|---|---|---|---|
| `id` | 16 | 22 | `A Q g w` |
| `S`, `L`, public keys, hashes | 32 | 43 | `A E I M Q U Y c g k o s w 0 4 8` |
| id token (§5.4) | 20 | 27 | `A E I M Q U Y c g k o s w 0 4 8` |
| challenge (§5.5) | 36 | 48 | any |
| signature | 64 | 86 | `A Q g w` |

A canonical 43-character encoding of 32 bytes never ends in `-` or `_`, so the design's rule "regenerate `S` when its last character is `-` or `_`" holds by construction.

### 1.2 Identifiers and secrets

- `id`: 16 bytes from the server CSPRNG, issued together with an id token by `GET /api/v1/instance` (§5.4, §6.6). Appears in the URL path and never in logs.
- `S`: 32 bytes from the client CSPRNG. Appears only in the URL fragment. The server MUST never receive `S`.
- `M`: 32 bytes from the client CSPRNG, the seed of the manage key. The client sends the public key `pk_M` at creation and signs cancel and status with `M`; `M` never leaves the client. A browser client lets WebCrypto generate the manage key pair as non-extractable, so `M` never exists as bytes in the page, and keeps it only as that `CryptoKey` (D39).
- `L`: 32 bytes from the server CSPRNG (lease token), issued by claim.

## 2. Key derivation (client)

Inputs: `id` (16 raw bytes), `S` (32 bytes), `kdf_id` (from the URL fragment, §4), optional passphrase.

```
hsalt   = SHA-256("nylzo/v1/hkdf-salt" || id)                            32 bytes
PRK_S   = HKDF-Extract(salt = hsalt, IKM = S)
R       = HKDF-Expand(PRK_S,  "nylzo/v1/read",     32)                  seed of the read key
salt_pw = HKDF-Expand(PRK_S,  "nylzo/v1/pw-salt",  32)                  passphrase salt
P       = kdf_id 0: empty (0 bytes)
          kdf_id 1: scrypt(pw, salt_pw, N = 2^17, r = 8, p = 1, dkLen = 32)
PRK_SP  = HKDF-Extract(salt = hsalt, IKM = S || P)
K       = HKDF-Expand(PRK_SP, "nylzo/v1/content",  32)                  content key
V       = HKDF-Expand(PRK_SP, "nylzo/v1/verifier", 32)                  seed of the verifier key
pk_X    = Ed25519 public key of seed X (RFC 8032 §5.1.5), for X in R, V, M
```

- `pw` is the passphrase normalised to Unicode NFC and encoded as UTF-8. Derivation accepts any well-formed passphrase, so a recipient can always open a message and the vectors stay valid. A client MUST refuse to create a message with a passphrase shorter than 12 characters (Unicode code points after NFC) and SHOULD warn unless the passphrase has at least 16 characters and at least 4 words (D37); both cores expose this check.
- The passphrase MUST be well-formed Unicode. A core rejects invalid UTF-8 or a lone surrogate before it derives anything, so two cores never derive different keys from the same input.
- The string literals are ASCII without a terminating NUL. `info` strings are passed to HKDF-Expand verbatim.
- `S || P` has a fixed length per profile (32 or 64 bytes), so the concatenation is unambiguous.
- `kdf_id` 2 is reserved and MUST NOT be assigned again. It named PBKDF2-HMAC-SHA-256 with 600,000 iterations, which Nylzo dropped before its first release (D71), so a passphrase always uses profile 1. A client MUST NOT derive anything for `kdf_id` 2 (§4).
- `K` MUST never leave the client. In the browser the client MUST derive `K` as a non-extractable `CryptoKey` (`deriveKey`, `extractable = false`) and use `deriveBits` only for `R`, `salt_pw` and `V`.
- `pk_R`, `pk_V` and `pk_M` are sent at creation. `R`, `V` and `M` never leave the client; every authenticated request carries a public key and a signature over a single-use server challenge instead (§5.5), so a recorded request cannot be replayed.
- `kdf_id` for derivation MUST come from the URL fragment. A client MUST NOT derive `V` with a profile supplied by the server; when the server reports a different profile for a live message (metadata in state `available` or `opened`, or `X-Nylzo-Policy`), the client MUST stop and MUST NOT send any verifier signature. Tombstone metadata reports `kdf_id` 0, which is no profile; a client MUST NOT compare it.

## 3. Envelope

```
header     = version (1 byte, 0x01) || kdf_id (1 byte) || flags (1 byte) || iv (12 bytes)      15 bytes
policy     = u16be(opens_max) || u32be(ttl_s)                                                    6 bytes
aad        = header || id (16 bytes) || policy                                                  37 bytes
plaintext  = u32be(len_meta) || meta || file_bytes || padding
ciphertext = AES-256-GCM-Encrypt(K, iv, plaintext, aad)                                         plaintext + 16 bytes
object     = header || ciphertext
```

- `version` MUST be `0x01`. A decoder MUST reject any other value.
- `kdf_id` MUST be 0 or 1 and MUST equal the profile in the URL fragment.
- `flags`: bit 0 is reserved for the HPKE mode and MUST be 0 in v1; all other bits MUST be 0. A decoder MUST reject non-zero flags. No flag reveals whether a message carries a file, but the object length does: text-only plaintext is padded to 4 KiB and file plaintext to 64 KiB, so apart from one shared length (65567 bytes) the length of an object shows whether a file is attached (D16).
- `iv`: 12 bytes from the CSPRNG, fresh per message. One `K` encrypts exactly one envelope.
- `opens_max` and `ttl_s` are the sender's policy. The recipient obtains them from `X-Nylzo-Policy` (§6.4) and the metadata response and decrypts with them; any change by the server or an intermediary fails authentication.
- `len_meta` is the byte length of `meta`.
- `file_bytes` are present exactly when `meta.file` is present and have length `meta.file.size`.
- `padding` is zero bytes. The padded length is visible to the server, so the size of a file is known to 64 KiB and the size of a text to 4 KiB.
- The plaintext length (`4 + len_meta + len(file_bytes) + len(padding)`) MUST be a multiple of 4096 bytes; when `meta.file` is present it MUST be a multiple of 65536 bytes. The decoder MUST derive the content length from `len_meta` and `meta.file.size`, MUST NOT depend on the padding content, and MAY ignore it.
- Limits: `meta.text` at most 102400 bytes of UTF-8; `meta.file.size` at most 5242880; `len_meta` at most 104448; `object` at most 5410880 bytes (5 MiB + 100 KiB + 64 KiB + 64 B). These are protocol maxima that both reference cores enforce. An instance MAY report lower limits in `/api/v1/instance` and MUST NOT report higher ones. An instance derives `max_object_bytes` from its file limit as the largest object a valid meta and file can produce: `min(5410880, 15 + 16 + pad65536(4 + 104448 + max_file_bytes))`, where `pad65536` rounds up to a multiple of 65536. With the default file limit that is 5373983.

### 3.1 meta **[detail]**

`meta` is a JSON object:

```json
{"v":1,"text":"…","file":{"name":"…","mime":"…","size":N}}
```

- `v` MUST be 1. `text` and `file` are each optional; at least one MUST be present.
- `file.name`: 1 to 255 bytes of UTF-8, no `/`, `\`, C0 control characters (U+0000 to U+001F) or DEL (U+007F). Clients MUST additionally sanitise the name before display and download, and display only the sanitised name, isolated from the direction of the surrounding text (D35).
- `file.mime`: 0 to 255 bytes. `file.size`: non-negative integer equal to the length of `file_bytes`.
- Encoders MUST produce the canonical form so both cores emit identical bytes: UTF-8, no whitespace, keys in the order `v`, `text`, `file` (and `name`, `mime`, `size` inside `file`), absent keys omitted, integers in decimal without sign, exponent or fraction. String escaping: `"` → `\"`, `\` → `\\`, U+0008 → `\b`, U+0009 → `\t`, U+000A → `\n`, U+000C → `\f`, U+000D → `\r`, every other code point below U+0020 → `\u00XX` with lowercase hex, every other code point literal UTF-8 (including `/`, U+2028 and U+2029).
- Decoders MUST accept any RFC 8259 JSON object that satisfies the constraints above, MUST ignore unknown keys, and take the last occurrence of a duplicated key. They MUST reject meta nested deeper than 32 levels; the top-level object is level 1 and brackets inside strings do not count.

## 4. Link

```
https://{host}/m/{id}#1.{kdf_id}.{S}
```

- Path `/m/{id}` with `id` in canonical base64url (22 characters). The server serves a static page for it; loading it consumes nothing.
- Fragment: fragment version `1`, then `kdf_id` (`0` or `1`), then `S` in canonical base64url (43 characters), separated by `.`. The fragment never leaves the browser in any request.
- A client MUST reject an unknown fragment version, an unknown `kdf_id`, or a non-canonical `S`.
- A newer fragment version gets its own error, so that the client can say the link needs a newer client instead of calling it damaged. The version label is the text after `#` up to the first `.`, or the whole fragment without a `.`. It is newer when it is a decimal integer without a leading zero, at most 9 digits, and greater than the version the client implements. The client checks this before the path and the rest of the fragment, because a later version may change both, but only after the text before `#` passes the scheme and authority rules below with any path that starts with `/`. Every other label, such as `0`, `01` or `v2`, is an invalid link.
- A link or fragment that would be valid but carries the reserved `kdf_id` `2` (§2) gets its own error too, so that the client can say the link uses a passphrase profile Nylzo no longer supports instead of calling it damaged. The client sends no request for such a link.
- A client parses a link without URL normalisation. The text before the first `#` MUST match `scheme "://" authority path ["?" query]`: `scheme` is `http` or `https` in any letter case, `authority` contains only ASCII letters, digits, `.`, `-`, `:`, `[` and `]`, `path` is exactly `/m/{id}`, and the query is ignored. The fragment is everything after the first `#`, so a link with `##` carries the unknown fragment version `#1` and is invalid. In the scheme, authority and path, relative references, dot segments, backslashes, userinfo, percent-encoding and leading or trailing whitespace are therefore rejected; the query may contain any character except `#`. Both reference cores accept the same set of links.

## 5. Server-side tokens and secrets

### 5.1 Pepper and purpose keys

- `pepper`: 32 bytes of configuration (`NYLZO_PEPPER`, or the file named by `NYLZO_PEPPER_FILE`), never stored in the database. The server refuses a pepper whose 32 bytes are all equal, the typical placeholder value.
- `k_purpose = HKDF-SHA-256(salt = empty, IKM = pepper, info = "nylzo/v1/mac/" || purpose, 32)` for `purpose` in `read`, `verifier`, `manage`, `lease`, `pow`, `id`, `challenge`.
- `mac_purpose(x) = HMAC-SHA-256(k_purpose, x)`.
- `pepper_id = SHA-256("nylzo/v1/pepper-id" || pepper)[0]` (one byte). Every row that stores a mac carries the `pepper_id` of the key that produced it.
- Rotation: `NYLZO_PEPPER_PREVIOUS` holds the previous pepper. The server writes with the current pepper and verifies each row with the pepper whose `pepper_id` matches the row; a row whose `pepper_id` matches neither verifies as invalid (with the same work as a valid row). The server MUST refuse to start when both peppers share a `pepper_id`. The maximum TTL is 7 days, so no rewriting is needed: after 7 days no live message carries the previous `pepper_id`. Tombstones keep it for another 7 days; dropping the previous pepper earlier turns those tombstones into unknown ids (404) without affecting any live message. An expired message becomes a tombstone when the next request or sweep ends it, so keeping the previous pepper for 14 days plus one sweep interval preserves every tombstone state.
- `sweep-once` does not need a pepper and never migrates the schema: it refuses a database with an older or newer `user_version`, so only `serve` migrates and a scheduler running a newer build cannot change the schema under an older server.

### 5.2 What the server stores

§13 lists everything the server keeps and what each item reveals.

| Client sends | Server stores or checks |
|---|---|
| `read_key = pk_R` at creation | stores `read_mac = mac_read(pk_R)` |
| `verifier_key = pk_V` at creation (passphrase only) | stores `verifier_mac = mac_verifier(pk_V)` |
| `manage_key = pk_M` at creation | stores `manage_mac = mac_manage(pk_M)` |
| `pk_R` and a signature with `R` at metadata, claim, blob, ack, burn, abuse | checks `mac_read(pk_R)` against the row, then the signature (§5.5) |
| `pk_V` and a signature with `V` at claim | checks `mac_verifier(pk_V)`, then the signature |
| `pk_M` and a signature with `M` at cancel, status | checks `mac_manage(pk_M)`, then the signature |
| (server issues `L` at claim) | stores `lease_mac = mac_lease(L)` |

The macs keep a copied database without the pepper from serving as a test for guessed keys: someone who has `S` and guesses a passphrase cannot compare `pk_V` without the pepper.

All comparisons MUST be constant-time. When a row does not exist, the server MUST still compute a mac and compare it against a dummy value so that the work and the response are identical.

Tombstones round `first_opened_at` and `ended_at` down to the minute, and `purge_at` is `ended_at + 7 d` from the rounded value. An ending finished at or after `expires_at` records `expires_at` as its end, so a tombstone never reveals when someone touched an expired link. No time in the database is more precise than the minute: `created_at` is rounded down, `expires_at` and `lease_expires_at` up to the next full minute, so a message and a lease live 0 to 60 s longer than requested.

Deleted rows leave no readable trace in the store. SQLite runs with `secure_delete`, `journal_size_limit = 0` and `wal_autocheckpoint = 1`: every commit is checkpointed into the database at once, and the next write starts the WAL over and cuts it to its own pages, so the WAL holds only the last transaction, whose pages the database already contains, and never the order of earlier writes (D20). A read that still uses an older snapshot stops the checkpoint at that snapshot; the transactions committed meanwhile stay in the WAL until the first write after the read has been checkpointed and the next one starts the WAL over. Every sweep also ends with `wal_checkpoint(TRUNCATE)`, which waits at most 250 ms for readers so it never holds the only writer connection for the full 5 s busy timeout, and counts a checkpoint that could not finish because a reader held the WAL as a failed step, so the next sweep tries again, and a database created by an older build is vacuumed once when it is first opened. `messages`, `tombstones` and `rate_counters` are tables without rowid, and every lease row gets a random row id from 2^32 to 2^63 − 1, so no table reveals the order or the number of creates, claims and other events. `messages.next_attempt_at` is 0 while no wait applies and is otherwise rounded up to the next full minute. No row ties a message id to an address bucket. SQLite keeps temporary tables, indices and the `VACUUM` copy in memory (`temp_store = MEMORY`), so no copy of rows is ever written to a temporary directory outside the data directory. Reading a blob does not update the access time of its file (`O_NOATIME` on Linux). What the file system or the storage provider keeps of deleted data is outside the server's control.

### 5.3 Address buckets

Addresses never reach a persistent log, the database or any file. For rate limits the server derives a 24-bit bucket in memory:

```
addr      = canonical IP (netip.ParseAddr; IPv4-mapped IPv6 unmapped to IPv4)
prefix    = addr masked to /32 or /24 (IPv4), /64 or /48 (IPv6)
input     = family (0x04 or 0x06) || prefix_len (1 byte) || masked address bytes (4 or 16)
bucket    = HMAC-SHA-256(k_addr, input)[0:3]
```

`k_addr` is 32 random bytes that every process draws when it starts and never writes anywhere; it does not come from the pepper. Every per-address token bucket and counter (§8) lives only in the memory of that process, so a copy of the database, a backup, the logs or the configuration, pepper included, holds no address and no value derived from one (D17). The price is availability: per-address limits count per instance and start from zero when a process restarts, also when a scale-to-zero container wakes up. The global daily cap and PoW (§8), passphrase backoff (§6.4) and the abuse report cap (§6.6) are kept in the database and never per address, so they hold across instances and restarts.

### 5.4 Id tokens

The server issues every message id, so nobody can choose the id of an earlier message and publish new content under its old link once its tombstone is gone, and a create cannot test whether an arbitrary id exists.

```
id_token = base64url( u32be(expires_at) || HMAC-SHA-256(k_id, id || u32be(expires_at))[0:16] )      20 bytes, 27 characters
```

- `expires_at` is the issue time plus 600 s, in Unix seconds. The id is 16 bytes from the server CSPRNG.
- A token is valid when the tag verifies in constant time under the current pepper and `0 ≤ expires_at − now ≤ 605 s` (5 s for clock differences between instances). A pepper rotation invalidates tokens issued before it; the client fetches a new one.
- The server keeps no record of issued ids. A token is not single-use, but its id can be stored only once: a second create with the same id answers 409.

### 5.5 Request signatures

Every request that proves possession of a link or of the manage key carries a signature over a fresh server challenge. A recorded request is worthless: its challenge is spent and a new one needs a new signature, which only the holder of the seed can make.

```
challenge = base64url( u32be(expires_at) || nonce (16 bytes) || HMAC-SHA-256(k_challenge, u32be(expires_at) || nonce)[0:16] )     36 bytes, 48 characters
message   = "nylzo/v1/request" || 0x00 || purpose || 0x00 || method || 0x00 || path || 0x00 || challenge (36 raw bytes) || SHA-256(body)
Authorization:    Nylzo-Sig {pk}.{challenge}.{signature}          signature = Ed25519(seed, message), purpose "read" or "manage"
X-Nylzo-Verifier: {pk_V}.{signature}                               claim with a passphrase only, purpose "verifier", same challenge
```

- `GET /api/v1/challenge` answers `{"challenge":"…","expires_at":"…"}`. `expires_at` is 60 s after issue, in RFC 3339 in the JSON and in Unix seconds inside the challenge. A challenge is valid when its tag verifies in constant time under the current pepper and `0 ≤ expires_at − now ≤ 65 s`.
- `purpose` is ASCII `read`, `verifier` or `manage`; `method` is the uppercase HTTP method; `path` is the request path without query, as sent (`/api/v1/messages/{id}/claim`); `body` is the raw request body, empty for GET.
- The server parses the header, checks the challenge, loads the row, compares the mac of `pk` with the stored mac, checks `pk` by the rule for creation (canonical encoding, not of small order, §6.2) and verifies the signature; a claim checks `pk_V` the same way. A row stored before the server refused such keys at creation therefore cannot be used with one either. It does all of this for every request, with a dummy row for an unknown id and a dummy key (a valid point of full order) for a malformed one or one that fails the key check, so every failure takes the same work and answers the same 404. Only a request that passed may then finish an interrupted ending or release a blob that is no longer reachable (§6.3), so a known id with a wrong key takes the same work as an unknown id.
- Every signed request except `POST /api/v1/abuse` has an empty body. A body, or a malformed `X-Nylzo-Verifier`, answers 400 before the signature is checked.
- The first request that presents a challenge whose tag verifies inside its window spends it, before any other check and whatever the answer: a 429 from a token bucket or a counter, a 503, a 400, a 404 and a success all leave it spent, so no request that the server turned away can be sent again by someone who recorded it. The server records the nonce in `rate_counters` with `window_start = 0` and `purge_at = expires_at + 60 s` rounded up to the full minute, so every instance, even with its clock 5 s off, has stopped accepting the challenge before the row is purged. A second request with the same challenge fails like a wrong key. A claim spends its challenge once for both signatures.
- Spends that arrive together share one transaction (D21). A process writes one batch of spends at a time, with a separate result for each nonce. A spend that arrives while no batch is being written is written at once; spends that arrive during a write wait for it and then go together in the next transaction, at most 256 per transaction. Every request waits until its batch has committed, so a spend is durable and visible to every instance before the request goes on. A request whose client goes away stops waiting; its spend is still written. An idle process adds no delay, and a burst of fresh challenges costs one write per batch instead of one per challenge.
- Every process also remembers the nonces it has seen until they expire, so repeating a challenge costs no database write and the spend can run before the token buckets. The memory holds at most 65536 nonces; when it is full it drops expired ones, then a random eighth, and a dropped nonce costs one row again when it comes back. Only challenges that the server issued count, and issuing them goes through the token buckets, so the spends add at most one row per issued challenge.
- When the store cannot record a batch, or the write fails in any other way, the server answers 500 to every request in it and that process refuses those challenges from then on. Another instance could still accept such a challenge until the store works again, at most until it expires.
- A client fetches a fresh challenge for every request. A 404 can also come from a challenge that expired on the way or was issued before a pepper rotation, so a client may retry once with a fresh challenge before it reports the link as unknown.
- Pepper rotation invalidates challenges issued before it; the client fetches a new one.

## 6. HTTP API

Base path `/api/v1`. Request and response bodies are JSON (`application/json; charset=utf-8`) except where noted. Timestamps are RFC 3339 in UTC. Binary values use canonical base64url (§1.1). "Signed with R" means an `Authorization: Nylzo-Sig` header made with the seed `R` (§5.5).

| Method and path | Auth | Consumes an open |
|---|---|---|
| `POST /api/v1/messages` | none (rate limit, PoW under load) | no |
| `GET /api/v1/messages/{id}` | signed with R | no |
| `POST /api/v1/messages/{id}/claim` | signed with R (+ `X-Nylzo-Verifier` signed with V) | yes |
| `GET /api/v1/messages/{id}/blob` | signed with R + `X-Nylzo-Lease` | no |
| `POST /api/v1/messages/{id}/ack` | signed with R + `X-Nylzo-Lease` | no |
| `POST /api/v1/messages/{id}/burn` | signed with R | ends the message |
| `POST /api/v1/messages/{id}/cancel` | signed with M | ends the message |
| `GET /api/v1/messages/{id}/status` | signed with M | no |
| `GET /api/v1/instance` | none | no |
| `GET /api/v1/challenge` | none | no |
| `POST /api/v1/abuse` | signed with R (rate limit) | no |

No endpoint accepts or returns plaintext, `S`, `K` or a passphrase.

### 6.1 Errors

| Status | Body | When |
|---|---|---|
| 400 | `{"error":"bad_request"}` | malformed `X-Nylzo-Create` (including the id inside it), request body or `X-Nylzo-Verifier`, a body on a signed request other than abuse (§5.5), policy outside instance limits, `size` ≠ Content-Length, version or kdf mismatch, an abuse report without `Content-Type: application/json` |
| 403 | `{"error":"passphrase","retry_after":N,"attempts_left":N}` | wrong verifier or backoff active; no open consumed |
| 403 | `{"error":"pow"}` | PoW required and missing, invalid or already used |
| 403 | `{"error":"id_token"}` | create with an id token that is missing, invalid or expired; the client fetches a new id from `GET /api/v1/instance` |
| 404 | `{"error":"not_found"}` | unknown id, a key that does not match, a missing, malformed, expired or spent challenge, a bad signature, and a malformed id in the path (indistinguishable) |
| 409 | `{"error":"conflict"}` | id exists in `messages` or `tombstones` (identical for both) |
| 410 | `{"error":"gone","state":"opened\|expired\|cancelled\|deleted"}` | message ended |
| 410 | `{"error":"gone","state":"available\|opened"}` | blob or ack with a lease that is not active while the message is still live; `state` is the message's current state, read again after the lease check, so a message that ended meanwhile answers its end state |
| 411 | `{"error":"length_required"}` | create without Content-Length |
| 413 | `{"error":"too_large"}` | Content-Length above the instance limit |
| 429 | `{"error":"rate_limited","retry_after":N}` + `Retry-After` header | bucket limit, source, prefix or instance token bucket, 4th blob download in a lease |
| 500 | `{"error":"internal"}` | storage or blob failure; the body never carries details. A request whose client went away answers 400 instead. A claim whose transaction committed always answers 200 and sends `X-Nylzo-Lease` before the body, so a client whose download breaks resumes through `/blob` without spending another open. A live, unexpired message whose blob is missing without a recorded ending (§7) answers 500 and stays live; a claim checks this before it spends an open |
| 503 | `{"error":"unavailable"}` + `Retry-After` | global creation cap, `Sec-Purpose` requests, PoW verifier saturated, all stream slots busy for a claim or download (§9), abuse report files full for a report that would be stored |

The 404 response (status, headers, body bytes) MUST be byte-identical for an unknown id, a key that does not match the row and every failed signature or challenge. The 409 response MUST be identical for a live id and a tombstone, and for a conflict found before the body is read or while storing it.

### 6.2 Create

```
POST /api/v1/messages
Content-Type: application/octet-stream
Content-Length: {size}
X-Nylzo-Create: base64url(JSON {id, id_token, read_key, verifier_key?, manage_key, kdf_id, opens_max, ttl_s, size})
X-Nylzo-Pow: {token}            only when the instance reports pow.required
body: object (§3)
```

Server order of checks, each before reading the body: (1) parse the header and the id syntax, then verify the id token (§5.4, 403 `id_token`); (2) `Content-Length` present (411), equal to `size` (400), below the limit (413); (3) free disk space for this upload and the uploads in progress (503 with `Retry-After: 60`, §9), before any counter, so a client refused for disk space keeps its budget; (4) the per-address counters in memory (§8), counting the attempt even if it fails later (429 when over); (5) the global daily cap: 503 when the counter has reached the cap, PoW when it is at or above 50 % (403, §8.1), then one conditional increment reserves a slot (503 when none is left by then). A request that reserved a slot at or above 50 % without a verified PoW returns the slot and answers 403 `pow`; (6) id absent from `messages` and `tombstones` (409, same for both). The body may take `max(120 s, size / 16000 s)` to arrive, which is 128 kbit/s for the whole object. The server then streams the body through a size-bounded reader, reads the 15-byte header, stores `version` from it and rejects (400) a `kdf_id` that differs from the header field, stores the blob and the row with `created_at` and `expires_at = created_at + ttl_s` taken when the upload has been stored, so a slow upload does not use up the TTL (`created_at` rounded down, `expires_at` up to the full minute, §5.2), and answers:

```
201 {"id":"…","expires_at":"…"}
```

- `id` and `id_token` MUST come from one `GET /api/v1/instance` response (§5.4). A client never generates an id.
- `read_key`, `verifier_key` and `manage_key` are `pk_R`, `pk_V` and `pk_M` (§2). `verifier_key` MUST be present exactly when `kdf_id ≠ 0`.
- The server rejects (400) a key that is not the canonical encoding of a curve point, a key of small order (8 times the point is the identity) and two equal keys in one header. Correct clients never produce such keys; for a small-order key anyone could forge signatures without the seed, so a broken or hostile sender client must not be able to register one (D18). The check uses `filippo.io/edwards25519`, and every signed request repeats it for the keys it carries (§5.5).
- `opens_max` MUST be between 1 and the instance maximum; `ttl_s` between 60 and the instance maximum. The server MUST NOT clamp a policy; it rejects with 400.
- The header JSON MUST NOT contain unknown keys.
- Only stored messages keep their slot in the global cap. Every failure after the reservation, including a conflict, a bad body, a storage error and a client that disconnects, returns the slot. Concurrent creates therefore never exceed the cap; a process that crashes while it holds a slot leaves it taken until the UTC day ends. While uploads are in flight the counter includes their slots. A second counter holds stored messages only: when it has reached the cap, the 503 carries a `Retry-After` until the next UTC day, otherwise 60 s. The 60 s hint therefore also covers slots that a crashed process left taken, and it can repeat until the day ends.

### 6.3 Metadata (non-consuming)

```
GET /api/v1/messages/{id}
Authorization: Nylzo-Sig {pk_R}.{challenge}.{signature}
200 {"state":"available|opened|expired|cancelled|deleted","created_at":"…","expires_at":"…","opens_max":N,"opens_remaining":N,"ttl_s":N,"passphrase_required":bool,"kdf_id":N,"size":N,"first_opened_at":"…"?,"ended_at":"…"?}
404 for an unknown id and for any failed signature
```

Metadata never changes `opens_remaining` or the state. After the signature check passed, it MAY complete the deletion of a blob that is already unreachable (expired message, or `opens_remaining == 0` with no active lease), and it finishes an ending recorded in `messages.ending` (§7). Tombstones answer 200 with their state, `first_opened_at` (rounded to the minute, absent when the message was never opened) and `ended_at`. A tombstone keeps no policy or times, so `created_at` and `expires_at` are absent, `opens_max`, `opens_remaining`, `ttl_s`, `kdf_id` and `size` are 0 and `passphrase_required` is false. `kdf_id` 0 on a tombstone is not a profile (§2). While an ending is recorded but not yet finished (§7), metadata and status already report the ended state but keep the live fields and carry no `ended_at`; they switch to the tombstone shape once the ending is finished.

### 6.4 Claim

```
POST /api/v1/messages/{id}/claim
Authorization: Nylzo-Sig {pk_R}.{challenge}.{signature}
X-Nylzo-Verifier: {pk_V}.{signature}          only when passphrase_required, over the same challenge
body: empty
200
  X-Nylzo-Lease: {L}
  X-Nylzo-Policy: base64url(JSON {"opens_max":N,"ttl_s":N,"kdf_id":N,"flags":N})      [detail]
  Content-Type: application/octet-stream
  body: object
403 {"error":"passphrase","retry_after":N,"attempts_left":N}
410 {"error":"gone","state":"…"}
```

After it has checked that the request body is empty, and before the transaction, the server opens the blob and keeps it open for the response. A blob whose size differs from the stored `size` answers 500 and spends no open. A missing blob on a message that has ended (a recorded ending, or `expires_at ≤ now`) answers 410 with the end state and finishes the ending; on any other message it answers 500 and spends no open.

In one storage transaction:

1. Read `now` from the process clock, after the transaction holds the write lock, and load the row. A recorded ending, `state ≠ available` or `expires_at ≤ now` → 410.
2. With a passphrase: if the message has 100 failed attempts over its whole life, every claim answers 403 with `retry_after` until `expires_at` and `attempts_left = 0`; if `next_attempt_at` is in the future → 403 with that `retry_after`. Otherwise compare `mac_verifier(pk_V)` in constant time and require a valid verifier signature. On mismatch `fail_count_total += 1`; while `fail_count_total ≤ 5` answer 403 with `retry_after = 0` and `attempts_left = 5 − fail_count_total`; from the 6th failure set `next_attempt_at` to `now + min(2^(fail_count_total − 5), 3600)` seconds rounded up to the next full minute, and answer 403 with the resulting `retry_after` and `attempts_left = 0`. The counters belong to the message, not to an address, so a wait applies to every reader. No open is consumed. A claim without a verifier, with a verifier key that fails the key check (§5.5) or with a verifier signature that fails counts as a mismatch.
3. `opens_remaining −= 1`; insert a lease `(id, mac_lease(L), lease_expires_at = now + max(120 s, size / 16000 s) rounded up to the next full minute, downloads = 0)`. When `opens_remaining == 0` set `state = opened`.
4. Return the object with the lease and the policy.

A lease is active while `now < lease_expires_at`. The instance that served the claim schedules `ReleaseIfDone(id)` at `lease_expires_at + 1 s`. A claim or blob response may take as long as its lease: the write deadline is `max(120 s, size / 16000 s)` from the start of the response, so a recipient on a 128 kbit/s link still receives the largest object.

Anyone who holds the link but not the passphrase can keep this backoff running: every wrong attempt from the 6th on makes all readers wait, and after 100 failures the message stays locked until it expires. They learn nothing about the content or the passphrase, so this is a loss of availability only. The protocol has no reset of the counter, neither with the manage key nor by PoW for each attempt (D19): the holder of the link sets the waits off again within minutes, a reset would hand out up to 100 further passphrase guesses each time, and PoW costs a real recipient on a phone as much as it costs the attacker. The sender restores availability by cancelling the message and sending a new link through another channel.

Definition of one open: one successful atomic release of the envelope by the server after proof of the link (and the passphrase). Loading the page, metadata, a decryption attempt or a wrong passphrase never count.

### 6.5 Blob, ack

```
GET  /api/v1/messages/{id}/blob    signed with R, X-Nylzo-Lease: {L}
  200 object (also X-Nylzo-Policy), 410 when the lease is not active (§6.1) or the message ended, 429 from the 4th download in one lease
POST /api/v1/messages/{id}/ack     signed with R, X-Nylzo-Lease: {L}
  204; ends the lease early and calls ReleaseIfDone
```

Blob downloads count in the read rate limit and are capped at 3 per lease. The blob is deleted as soon as `opens_remaining == 0` and no lease is active. A claim or download that finds the blob already deleted by a concurrent ending, including an expiry, answers 410 without `X-Nylzo-Lease` and with the end state (§7), finishing that ending first if it was interrupted. A missing blob on an unexpired message without a recorded ending is a storage fault: the server answers 500, keeps the row and logs an error. A download that finds an ending already recorded answers 410 with that state and finishes the ending. `HEAD` on any `/api/` route, including instance and challenge, answers 404 like any other unsupported method, so it never spends a download, mints no id token or challenge and never reaches a signed handler. A stream that ends early is logged as `blob-stream`, without the number of bytes sent or the object size. The server finds a lease by message id, lease row and `lease_mac` together, so a stale lease token never touches a lease of another claim, even when the store reuses row numbers. An ack that finds an ending recorded, or a lease that a concurrent ending already removed, answers 410 with the end state and finishes the ending, like a download.

### 6.6 Burn, cancel, status, instance, abuse

```
POST /api/v1/messages/{id}/burn     signed with R → 204; state = deleted, blob deleted
POST /api/v1/messages/{id}/cancel   signed with M → 204; state = cancelled, blob deleted
GET  /api/v1/messages/{id}/status   signed with M → 200 {"state":"…","opens_used":N,"opens_max":N,"created_at":"…","expires_at":"…","first_opened_at":"…"?,"ended_at":"…"?,"failed_passphrase_attempts":N}
GET  /api/v1/instance               → 200 {"limits":{"max_file_bytes":N,"max_text_bytes":N,"max_object_bytes":N,"max_ttl_s":N,"default_ttl_s":N,"max_opens":N},"pow":{"required":bool,"difficulty":N,"challenge":"…"?},"create":{"id":"…","id_token":"…","expires_at":"…"},"versions":{"api":1,"envelope":1,"fragment":1,"server":"…"}}
GET  /api/v1/challenge              → 200 {"challenge":"…","expires_at":"…"} (§5.5)
POST /api/v1/abuse                  signed with R, {"id":"…","reason":"…"?} → 202 {"status":"accepted"} for known, unknown and malformed ids alike; a malformed id is answered before any rate counter
```

- `failed_passphrase_attempts` is cumulative over the message lifetime (`fail_count_total`), not per day.
- After the message ends, status reads the tombstone: only `state`, `first_opened_at` and `ended_at` are meaningful; `created_at` and `expires_at` are absent and the counters are 0.
- `first_opened_at` and `ended_at` are rounded down to the minute.
- A failed manage signature on cancel or status answers the same 404 as an unknown id.
- Abuse reports are rate limited to 5 per hour per bucket, reviewed by a human, and never delete anything automatically. After review the operator ends a message with `nylzo takedown` on the server host, which reads message ids from stdin, refuses the whole input when a line is not an id, and ends each message like burn, as `deleted` (D63). The reader and the sender's status then see `deleted` as after a burn, so a client describes `deleted` as deleted by a recipient or removed by the operator.
- A client MUST apply the redaction below to `reason` before it signs and sends a report, so a link or `S` pasted into the reason never reaches the server (§1.2); the server redacts again before storing.
- An abuse report MUST carry `Content-Type: application/json`, so a cross-site page needs a CORS preflight, which the server never grants. Before storing `reason`, the server percent-decodes it up to three times, replaces control characters with spaces, drops format characters such as bidi controls, replaces every `#…` run and every run of 22 or more base64url characters with `[redacted]`, then cuts it to 1000 bytes and further until its JSON encoding fits in 1200 bytes. A replacement character U+FFFD is dropped like an invalid UTF-8 sequence, so the client and the server redact the same text; the `redact` cases in `spec/vectors.json` pin this for both cores. It stores reports only for live messages: a report about an ended message (a tombstone) answers the same 202 and stores nothing, so no report outlives the 7 days after the end of its message (D46). It stores at most 3 reports per message per UTC day and answers 202 for the rest. The cap is per message only, so no row ties a message to a reporter's address; one reporter can therefore use up the daily budget of a message. The server reserves the count before it writes a report and returns it when the write fails. Reports go to one file per UTC day, `abuse/{YYYY-MM-DD}.jsonl` in the data directory, which the server only appends to and syncs after each report, so no report is ever rewritten. A valid report that would be stored but would push the report files together past 16 MiB answers 503, and the server logs the error `abuse-file-full` for the operator. A stored report holds the UTC date, the id and the redacted reason, never the time of day, and the server sets the modification time of the day file to the start of that UTC day after every write. Reports older than 7 days are dropped by deleting their day files, at every sweep including `sweep-once` and before every new report (D26, D46).

## 7. Lifecycle, leases, tombstones, sweep

```
 create ──▶ available ──claim──▶ available (opens_remaining > 0) | opened (opens_remaining == 0)
               │                                    │
               │ expires_at ≤ now | burn | cancel    │ last lease ended (ack or expiry)
               ▼                                    ▼
           tombstone {id, pepper_id, read_mac, manage_mac, state, first_opened_at, ended_at, purge_at = ended_at + 7 d}
               │ sweep after purge_at
               ▼
            unknown id (404)
```

- Every request checks `expires_at` and ends an expired message (lazy sweep).
- Ending a message takes three steps. (1) One transaction records the intended state in `messages.ending`; the first recorded ending wins, so a later burn, cancel or expiry finishes the recorded one and reports its state. `ReleaseIfDone` records `opened` when it hands out the blob of an unexpired message after the last lease. An expired row records nothing, because `expires_at ≤ now` already ends it durably; it ends as `opened` when no opens remain and as `expired` otherwise. (2) `Blobs.Delete`. (3) One transaction deletes the `messages` row and its `leases` and inserts the tombstone with the recorded state. A failed delete leaves the message alive with its ending recorded; claims, downloads, acks, burns and cancels answer 410 with that state, metadata and status report it, and every later request on the id and every sweep finishes the ending. A failure to finish is logged and never changes the response to a wrong key, which stays the uniform 404. Once burn or cancel has recorded its ending, the server finishes it even if the client disconnects. A burn or cancel that recorded the first ending answers 204 even when a concurrent release finishes that ending before it does.
- `ReleaseIfDone(id)` is idempotent: when `opens_remaining == 0`, no lease is active and `blob_ref` is set, it returns `blob_ref`, and keeps returning it until the caller, after a successful `Blobs.Delete`, clears it with `ClearBlob(id, blob_ref)`. `Blobs.Delete` is idempotent and treats a missing object as success. Callers: ack, the lease-end timer, every request on the id, and the sweep.
- Sweep (built-in goroutine every `NYLZO_SWEEP_INTERVAL`, default 5 min; or `nylzo sweep-once` from an external scheduler) ends expired messages, deletes expired leases, calls `ReleaseIfDone` for every message with `opens_remaining == 0` and a blob, finishes every recorded ending that a crash interrupted, purges tombstones after `purge_at`, purges expired `rate_counters`, in batches of 1000 rows paged by key, so rows that keep failing never hide the rows after them. The sweep ends the expired messages of a page together: one `Blobs.DeleteBatch` call deletes their blobs and syncs the blob directory once, then one transaction ends the rows whose blobs are gone. A message whose blob failed to delete stays until a later sweep or request ends it. Every sweep also deletes temporary upload files older than 15 minutes and abuse report files older than 7 days (§6.6). Once per UTC day the sweep lists the blob store and deletes objects older than 15 minutes whose ref no row holds. A counter in `rate_counters` records the day once a reconcile has listed both the rows and the blob store, so later sweeps that day, including separate `sweep-once` runs, skip it. A reconcile whose listing failed runs again on the next sweep; objects that failed to delete wait for the next day. The built-in sweeper also runs once at startup. Two sweeps that overlap may both reconcile, which is harmless. One failing item never stops the remaining steps, and `sweep-once` exits non-zero when any step failed.
- Clocks: every comparison of times (expiry, leases, backoff, counter windows, PoW windows) uses the clock of the process that handles the request or runs the sweep. All instances and sweep jobs MUST run with synchronised clocks; the reference store does not read a clock of its own. A claim passes its clock to the store, which reads it after the transaction holds the write lock, so a claim that waited for the writer never starts a lease after `expires_at`.
- Ids are never reused. The server issues every id from its CSPRNG with a short-lived token (§5.4), so after a tombstone is purged nobody can create a message under the old id, and the server still keeps no trace of a message longer than 7 days after it ends.
- Guarantees: access ends immediately (every request checks state, expiry and leases). Active data is deleted immediately on burn, cancel and the ack of the last lease; after the last lease expires without ack, at the end of the lease on a best-effort timer and at the latest by the next sweep.

## 8. Rate limits, caps, proof of work

- In-memory token buckets in front of the database for `/api/*`, checked in order: one per source, keyed by the /32 or /64 address bucket (§5.3; `NYLZO_RATE_SOURCE_RPS` default 10 requests per second, `NYLZO_RATE_SOURCE_BURST` default 20); one per prefix, keyed by the /24 or /48 bucket, with half the instance rate and burst; and one per instance (50 requests per second, burst 100). A request that an earlier bucket refuses takes no token from the later ones, so one /64 or /32 source, or one /48 or /24 range, cannot take more than half of the instance bucket. Many sources from different ranges can still drain it; the design does not claim availability against them. Each per-source and per-prefix table tracks at most 65536 entries; when full it drops entries that have refilled, then a random eighth. `/m/*` never queries the database and is not throttled. The id syntax is checked before any query.
- Counters per address bucket (§5.3), kept in the memory of each process with hourly and daily windows by the server clock. The /32 or /64 counters come first. A request over one limit is not counted against the limits after it, so a refused source does not use up its /24 or /48 budget. A refused request gets a `Retry-After` of at least 1 s. Each process tracks at most 65536 buckets per counter; when the table is full it drops buckets whose window has ended, then a random eighth, and a dropped bucket starts from zero. These limits count per instance and restart with the process (§5.3):

| Limit | /32 or /64 | /24 or /48 |
|---|---|---|
| create per hour | 20 | 100 |
| create per day | 100 | 500 |
| create bytes per day | 50 MB | 250 MB |
| metadata + claim + blob per hour | 300 | 1500 |
| abuse reports per hour | 5 | 25 |

- Global daily creation cap (`NYLZO_GLOBAL_DAILY_CREATES`, 0 = off): it counts stored messages plus creates still uploading (§6.2). From 50 % of the cap PoW is required for creation; at 100 % creation answers 503 while reading continues. `/api/v1/instance` reads the shared counter, so every instance reports the same state.
- The database keeps only counters without an address: the global daily counters, used PoW nonces, spent request challenges, abuse reports per message and day, and the daily reconcile marker. A daily row expires 24 hours after its window. A PoW nonce row has `window_start = 0` and expires 240 s after its challenge was issued, rounded up to the full minute. The sweep deletes expired rows in batches of 1000.

### 8.1 Proof of work **[detail]**

Algorithm per the design (OD24): scrypt with `N = 2^14, r = 8, p = 1` over a server-issued nonce bound to the message id and a 2-minute window.

```
challenge = base64url( u32be(window_start) || nonce (16 bytes) || HMAC-SHA-256(k_pow, u32be(window_start) || nonce)[0:16] )
token     = challenge || "." || u64be(counter) as 16 lowercase hex digits
valid     = HMAC tag verifies, −5 s ≤ now − window_start ≤ 120 s,
            and scrypt(password = nonce || id, salt = u64be(counter), N = 2^14, r = 8, p = 1, dkLen = 32)
            has its first `difficulty` bits equal to zero
```

- `GET /api/v1/instance` returns `pow.required`, `pow.difficulty` (default 4, at most 8) and, when required, a fresh `pow.challenge`. A client needs about 2^difficulty scrypt evaluations on average, so operators should keep the difficulty low enough for phones to finish within the 120 s window.
- A challenge is single-use. After the tag and window checks pass, the server records the nonce in `rate_counters`, then waits up to 10 s for a verifier slot and runs scrypt. A second token for the same challenge answers 403 without running scrypt. A failed create after that point, including a 503 because no slot freed up in time, needs a fresh challenge.
- The counter MUST be lowercase hex; a token with `A`–`F` in the counter is invalid.
- The server verifies with one scrypt call and bounds concurrent verifications (`NYLZO_POW_CONCURRENCY`, default 4, at most 4, since each one holds 16 MiB). The 5 s tolerance before `window_start` absorbs small clock differences between instances. A verification above that bound waits up to 10 s for a slot and then answers 503 with `Retry-After`.
- Clients that do not implement PoW receive 403 `{"error":"pow"}` while it is required.
- A client SHOULD fetch the challenge right before it starts solving, after its own key derivation, by calling `GET /api/v1/instance` again and ignoring the `create` part of that answer. It SHOULD stop solving shortly before 120 s after it fetched the challenge and fetch a new one, since the server refuses a token after the window.

## 9. Transport and headers

- `http.Server` timeouts: `ReadHeaderTimeout` 5 s, `ReadTimeout` 120 s, `WriteTimeout` 120 s, `IdleTimeout` 30 s, `MaxHeaderBytes` 8 KiB. A create body and its response, and a claim or blob response, get `max(120 s, size / 16000 s)` instead (§6.2, §6.4); the create response deadline starts after the body has arrived, so a slow upload still receives its 201. Unencrypted HTTP/2 (h2c) buffers at most 64 KiB of unread body per stream and 1 MiB per connection, so a create that has not started reading its body cannot hold back the other streams on its connection, and accepts frames of at most 16 KiB, so a peer cannot make a connection hold a larger read buffer. At most `NYLZO_MAX_STREAMS` claim and blob responses (default 64) stream at the same time per instance, because a client that stops reading holds its response until the write deadline. A claim or download beyond that answers 503 with `Retry-After: 5` after the rate limits and before the signature check, the same for every id, so it spends no open (D24). Each /32 or /64 address bucket may hold at most 2 of these slots and each /24 or /48 bucket at most 8 (D49), so one source that stops reading cannot take every slot. Bodies are bounded by a size-limited reader at the instance object limit. The server never decompresses request bodies. A create answers 503 with `Retry-After: 60`, before the per-address counters and the global cap count it, when the free space of the data directory minus the declared sizes of the uploads in progress and of this one would fall below `NYLZO_MIN_FREE_BYTES` (default 1 GiB), so a full disk never stops the writes that challenge spends, burn and cancel need, even while many uploads run at once (D48). An upload holds its reservation until its handler returns.
- Client address: taken from the connection. `X-Forwarded-For` is honoured only when the connection comes from `NYLZO_TRUSTED_PROXIES` (CIDR list), taking the rightmost value that is not a trusted proxy. `NYLZO_PROXY_MODE=scaleway` reads `X-Real-IP` instead, only when the request carries exactly one such value and, if `NYLZO_TRUSTED_PROXIES` is set, only from a connection inside it; otherwise the connection address counts. This mode is safe only behind an ingress that sets the header and that every connection passes through. Without configuration both headers are ignored. When a request carries `X-Forwarded-For`, `X-Real-IP` or `Forwarded` and the server still takes the connection address as the client address, it logs `forwarded-header-ignored` at warning level with the name of the first such header, at most once per hour (D69). That covers a proxy that is not trusted, a trusted proxy that sends only `X-Real-IP` or `Forwarded` or an unreadable `X-Forwarded-For`, and `NYLZO_PROXY_MODE=scaleway` without `X-Real-IP`: in each case all clients share the proxy's bucket. A client that sends such a header on a connection that bypasses the trusted proxy, or to a server without a proxy, causes the line too, and so does the rare `X-Forwarded-For` that lists only trusted addresses. A trusted proxy that passes a client's own `X-Real-IP` or `Forwarded` along does not, because the address then comes from `X-Forwarded-For`.
- Every response carries: `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `X-Robots-Tag: noindex, nofollow` (except the home page, see below), `X-Content-Type-Options: nosniff`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin`, `Strict-Transport-Security: max-age=31536000; includeSubDomains`, `Permissions-Policy: accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()`.
- The home pages (`GET /` and `GET /cs/`, and `HEAD` on both) carry no `X-Robots-Tag`, so search engines may index them; every other response keeps `noindex, nofollow`, and the web client's HTML has no robots meta tag (D80, D82).
- Responses other than HTML carry `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`.
- HTML responses carry `Content-Security-Policy: default-src 'none'; script-src 'nonce-{nonce}'; style-src 'nonce-{nonce}'; connect-src 'self'; img-src 'self'; font-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; require-trusted-types-for 'script'; trusted-types svelte-trusted-html nylzo-kdf` with a fresh nonce of at least 128 bits per response.
- `worker-src 'self'` admits the module worker in which the web client runs scrypt for profile 1 and proof of work, one worker per derivation, terminated after it (D28). `trusted-types` admits only the policy that the Svelte runtime creates for component templates and the policy `nylzo-kdf` that creates the worker URL. A worker script is a non-HTML response and gets `default-src 'none'`, so it MUST be a single file without imports.
- `GET /`, `GET /cs/`, `GET /sent`, `GET /report`, `GET /transparency` and `GET /m/{id}` (§4) serve the page of the embedded web client with the HTML CSP above and a fresh nonce; `GET /cs` answers 308 with `Location: /cs/`. The client chooses the screen by path: create, the sender's list of remembered messages, an abuse report from a pasted link, what the instance stores, logs and limits (§10, §13), and reading. The create screen has one address per language, English on `/` and Czech on `/cs/` (D82), and both come prerendered in their language from the templates `home.en.html` and `home.cs.html` of the build (D83); the other pages get the template `index.html`, whose body is empty, and choose their language in the browser.
- `NYLZO_PUBLIC_URL`, when set, is the address of the instance as visitors reach it: `https://` with a host and an optional port and nothing after it, or `http://` with the host `localhost`, `127.0.0.1` or `[::1]` for development; the host is lowercase ASCII. The server refuses to start with any other value. The templates mark the parts that need an absolute address (canonical link, `hreflang` links, `og:url`, `og:image`, structured data) with `<!--nylzo:public-->` and `<!--/nylzo:public-->`; at startup the server replaces every `__NYLZO_PUBLIC_URL__` in them with the public URL and drops the markers, or without a public URL drops those parts with their markers (D84).
- `GET /robots.txt` answers `text/plain; charset=utf-8` with `User-agent: *`, `Disallow: /api/` and, with a public URL, `Sitemap: {public URL}/sitemap.xml`. It does not disallow `/m/`, so a crawler that meets a message link sees its `noindex`. `GET /sitemap.xml` answers, only with a public URL, `application/xml; charset=utf-8` listing `/` and `/cs/` with `xhtml:link` alternates for `en`, `cs` and `x-default` (`/`); without a public URL it answers 404 (D84).
- `GET /assets/{name}` serves the files of the embedded web client by exact name: `.js` as `text/javascript; charset=utf-8`, `.css` as `text/css; charset=utf-8`, `.woff2` as `font/woff2`, and the icons `.svg` as `image/svg+xml` and `.png` as `image/png` (D72). An SVG opened on its own is a non-HTML response with `default-src 'none'`, so it runs no script and loads nothing. Any other name, including one that decodes to a path, answers 404. The server refuses to start when the embedded build holds a file of another type, a directory or a precompressed variant without its file, or when one of its templates (`index.html`, `home.en.html`, `home.cs.html`) is missing, lacks the nonce placeholder, holds unbalanced public markers or references an asset that is not embedded; without a build it serves a placeholder page on every page address and logs that. When `Accept-Encoding` accepts `gzip`, the server sends the gzip variant from the build with `Content-Encoding: gzip`; every asset response carries `Vary: Accept-Encoding` and, like every response, `Cache-Control: no-store` (D36). The server checks at startup that every gzip variant unpacks to its file, and the build carries no other encoding, because the server could not check it (D47). Assets pass no token bucket, since they never reach the database (§8).
- `GET /.well-known/nylzo/client.json` answers the manifest of the embedded web client (D60): `{"server":"…","built":bool,"nonce_placeholder":"__NYLZO_CSP_NONCE__","public_url":"…","pages":["/","/cs/","/sent","/report","/transparency","/m/{id}"],"files":[{"path":"/","sha256":"…"},{"path":"/cs/","sha256":"…"},{"path":"/sent","sha256":"…"},{"path":"/assets/{name}","sha256":"…"},…]}` with the assets sorted by name; `public_url` is `NYLZO_PUBLIC_URL` or empty. The hashes of `/`, `/cs/` and `/sent` are the SHA-256 of `home.en.html`, `home.cs.html` and `index.html` as built, with the nonce and public URL placeholders and the markers, because every page response carries a fresh nonce and the instance's own address in their place; `/report`, `/transparency` and `/m/{id}` use the template of `/sent`. `verify-client` checks the public URL against the same rule as the server and builds each expected page from its own template the same way; the hash of an asset is the SHA-256 of its uncompressed file. Without a build `built` is false and `files` is empty. The manifest is the server's own claim and proves nothing on its own: a client checks the served files against a source it trusts, the release or its own embedded copy (`nylzo verify-client`, D61). `verify-client` sees only what the server sends to it; a server can recognise it by its headers and TLS fingerprint and send browsers something else, so the check finds a swap that does not avoid the verifier, such as one from a compromised build or a broken deployment, not a server that hides it on purpose. It requires the exact headers of this version on every page and file, each once, a different nonce in every page response and as many nonce occurrences as the template has placeholders; a header this version never sends is a mismatch unless a proxy adds it (`Date`, `Content-Length`, `Server`, `Via`, `Alt-Svc`, `Connection`, `Keep-Alive`, or one the operator allows with `--allow-header`), and headers that change what a browser does (`Refresh`, `Set-Cookie`, `Clear-Site-Data`, `Link`, `Content-Disposition`, `NEL`, `Report-To`, `Reporting-Endpoints`, `Speculation-Rules`, `Service-Worker-Allowed`, `Accept-CH`, `Critical-CH`) can never be allowed.
- `GET /.well-known/nylzo/transparency.json` answers `{"server":"…","operator":{"name":"…"?,"contact":"…"?,"hosting":"…"?}?,"limits":{as in GET /api/v1/instance},"rates":{"create_per_hour":N,"create_per_day":N,"create_bytes_per_day":N,"read_per_hour":N,"abuse_per_hour":N,"coarse_multiplier":N,"global_daily_creates":N,"pow_difficulty":N},"retention":{"tombstone_s":N,"abuse_report_s":N,"daily_counter_s":N}}` from the configuration and the constants the server uses (D62); 0 for a rate means no limit. `operator` appears only when at least one of `NYLZO_OPERATOR`, `NYLZO_CONTACT` and `NYLZO_HOSTING` is set. The server refuses to start when one of them is longer than 200 bytes or holds invalid UTF-8, a control or a format character, or when the contact matches neither `^[A-Za-z0-9_%+-]+(\.[A-Za-z0-9_%+-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$` (an email address) nor `^https://[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*(:[0-9]{1,5})?(/[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]*)?$` (an https address without user information); a client links the contact only when it matches the same expressions, as `mailto:` or as the address itself. Both documents carry the headers of non-HTML responses, need no database and pass no token bucket, like the assets.
- Requests with a `Sec-Purpose` header to `/m/*` or `/api/*` answer 503. Known link-preview user agents receive, for `/m/*`, minimal HTML without script.

## 10. Logging

The server logs time, truncated to the minute (D50), method, route pattern and status, and the latency for every route except create, claim and blob, because their latency follows the object size and would let a log line be joined with `messages.size`. It never logs the size of a response or of an object: for create, claim and blob the size is the object, and the length of a metadata or status answer follows the number of digits in its size, TTL and counters and whether a passphrase is required. It never logs a message id, so a log line names no message. With very little traffic the minute of a line can still match the minute of a message's `created_at` or `first_opened_at`, and the platform that collects the log adds its own timestamps, possibly finer ones. A proxy that terminates TLS in front of the server logs by its own rules; the Compose example configures Caddy to log no requests and no errors with request details. Storage errors are logged as the failing step, operation and system error only (for example `delete blob: remove: read-only file system`), never with a file path, because a blob path holds the id in hex. It never logs the raw path, so a mangled link whose fragment reached the path is not logged. Requests that a middleware answers before routing (token-bucket 429s and `Sec-Purpose` 503s) are logged without route. Internal `net/http` errors are logged as a fixed message with a category (`accept`, `panic`, `tls-handshake`, `http2`, `superfluous-write-header`, `other`; HTTP/2 handler panics count as `panic`) and without their text, because that text can carry a peer address. Warning and error categories are rate limited separately, each to a burst of 20 and 1 per second, and the next logged line reports how many were dropped. A failed sweep logs its report, the number of failed items and the first error. It MUST NOT log `Authorization`, `X-Nylzo-Create`, `X-Nylzo-Lease`, `X-Nylzo-Verifier`, `X-Nylzo-Pow`, request or response bodies, query strings, or client addresses. Addresses exist only in process memory (§5.3). The `forwarded-header-ignored` line (§9) holds only the header name.

## 11. Versioning

Envelope version in the first byte, fragment version before the first dot, API under `/api/v1`, `kdf_id` in the envelope header and the fragment. A client MUST reject an unknown version or non-zero flags. A new KDF profile or the HPKE mode is a new value in an existing field, not a new format. A retired value stays reserved and is never reused (`kdf_id` 2, §2).

## 12. Data model (reference)

```
messages(id TEXT PK, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining,
         pepper_id, read_mac BLOB, manage_mac BLOB, verifier_mac BLOB NULL, size, blob_ref TEXT NULL,
         fail_count_total, next_attempt_at, first_opened_at NULL, ending TEXT NULL)   WITHOUT ROWID
leases(row_id INTEGER PK (random, 2^32 to 2^63 − 1), id FK, lease_mac BLOB, pepper_id, lease_expires_at, downloads)
tombstones(id TEXT PK, pepper_id, read_mac, manage_mac, state, first_opened_at NULL, ended_at, purge_at)   WITHOUT ROWID
rate_counters(key TEXT, window_start, count, bytes, purge_at)   WITHOUT ROWID
```

The database records its schema version in `PRAGMA user_version`, currently 4. A server refuses to open a database whose version is newer than its own. Version 4 adds triggers that abort every write by which an older build would keep more than this one: a per-address counter key in `rate_counters` (§5.3), a PoW nonce row with a non-zero `window_start`, and a lease row whose row id is missing or below 2^32. An older build that opens an upgraded database therefore fails its creates, metadata, claims, blob downloads and abuse reports, all of which wrote per-address counters, instead of writing such rows.

Blob store: one object per message. `blob_ref` is the message id. On disk the file name is the lowercase hex of the ref bytes (`data/blobs/{hex(ref)}`), so case-insensitive file systems cannot merge two ids; on S3 the object key is the ref. Publishing an object never replaces an existing one: a second create with the same id answers 409 and leaves the first object intact.

## 13. What a stolen server reveals

This section lists everything the server keeps, so an operator, an auditor or an attacker who copies the server sees the same picture. "Stolen" means a copy of the data directory (SQLite database with its WAL, the blob directory with its temporary uploads, the abuse report files), the logs and the configuration including the pepper. `TestStolenServerRevealsNothing` runs every flow (with and without passphrase, with a file, opened, leased, expired, burned, cancelled, reported), copies these files and the log, and checks that none of them contains `S`, `K`, `P`, a passphrase, plaintext, a file name, the seeds `R`, `V` or `M`, the public keys `pk_R`, `pk_V` or `pk_M`, `L`, a client address, a message id in hex in the log, or a time finer than the minute, and that `rate_counters` holds only the address-free keys listed below. It runs with PoW required for one create and with a failing blob delete, so the error paths are covered too.

| Item | Kept | What it reveals | Why it does not help to reach content |
|---|---|---|---|
| Pepper (`NYLZO_PEPPER`, previous pepper) | configuration | With it, candidate public keys can be checked against the stored macs and id tokens, request challenges and PoW challenges minted. A minted id token lets its holder create a message under any id whose tombstone is gone, so together with an old link it can publish new content under that link (integrity, not confidentiality; rotate the pepper after any suspected leak). Address buckets do not come from it (§5.3) | Every key the macs protect comes from `S` (and `P`) or from 256 random bits (`M`); without the link there is nothing to test, and a minted challenge still needs a signature with the seed |
| `messages`: id, version, `kdf_id`, flags, state, `opens_max`, `opens_remaining`, `ttl_s`, size, ending | until the message ends | That a message with this id exists, whether it needs a passphrase, its policy and its padded size (text to 4 KiB, file to 64 KiB). The table has no rowid, so it does not show the order of creates | The id is public in the link; none of these values enters a key |
| `messages`: `created_at`, `expires_at`, `first_opened_at`, `next_attempt_at` | until the message ends | Times to the minute (§5.2) | No time enters a key |
| `messages`: `fail_count_total` | until the message ends | How many wrong passphrases were tried | Says nothing about the passphrase |
| `messages`: `read_mac`, `manage_mac`, `verifier_mac`, `pepper_id` | until the message ends, the first two also in the tombstone | `HMAC(k_purpose, pk)` of the read, manage and verifier public keys | Without the pepper the values are random to the attacker; with it they only confirm a key the attacker already derived from `S` |
| `leases`: random row id, `lease_mac`, `lease_expires_at`, downloads | until the ack, the end of the lease or the next sweep | That a claim happened, to the minute; the row id is random, so it does not count or order claims | `L` is random and only the mac is kept |
| `tombstones`: id, state, `first_opened_at`, `ended_at`, `purge_at`, `read_mac`, `manage_mac` | 7 days after the end | How and roughly when a message ended | Same as above; no content and no key material |
| `rate_counters` | 24 hours after a daily window; PoW nonces 4 to 5 minutes; spent request challenges 2 to 3 minutes | The global daily counters, used PoW nonces, spent request challenges (one random nonce each, no id), the number of abuse reports per message and day, the daily reconcile marker | No row holds an address or a value derived from one, with or without the pepper (§5.3); the challenge rows only show how many signed requests arrived in the last few minutes |
| `nylzo.db-wal`, `nylzo.db-shm` | until the next write starts the log over; both files go when the database closes cleanly | The pages of the last transaction, which the database already holds; during a read, also the transactions committed since its snapshot (§5.2). The headers count the write transactions since the database was opened, so two copies taken at different times give the number of writes between them | Its pages hold rows of the tables above, so it adds no key material and no order of earlier creates and claims |
| `blobs/{hex(id)}`, `blobs/tmp/*` | until the last lease ends, the message is burned or cancelled, or it expires | The envelope: header (version, `kdf_id`, flags, IV) and AES-256-GCM ciphertext of padded plaintext | Decryption needs `K`, which needs `S` (and `P`) |
| `abuse/{YYYY-MM-DD}.jsonl` | 7 days, and never past the end of the reported message plus 7 days (D46) | The UTC date, the id and the redacted reason of each stored report | No address; the file's modification time is the start of its day, links and long tokens are removed from the reason |
| Request log | as long as the operator keeps it | Time, method, route pattern, status; latency except for create, claim and blob; no response size | No id, no address, no token, no body, no file path (§10). With very little traffic the time of a create or claim line can match a message's minute |
| File system metadata | as long as the file exists | The server sets the `mtime` of a blob file to the minute (D50), but its `ctime`, the `ctime` of an abuse day file and the `mtime` of the directories and of `nylzo.db` show the time of the last such write to the nanosecond, which on a quiet instance names the message; freed blocks may keep old ciphertext | Outside the server's control; an encrypted volume limits it. Ciphertext without `S` stays useless |

What goes beyond stolen data, stated plainly:

- An attacker who controls the running server or the provider edge (T3, T13) can serve a modified web client to a chosen victim and read that victim's links and passphrases. No server design prevents this. The CLI, a separate static origin for the web client (OD23, planned for the web client milestone), a published manifest of the client files and an independent monitor make it harder or visible.
- While a process runs, its memory holds `k_addr` and the per-address counters of the current windows (§5.3); someone who controls the running process sees client addresses anyway.
- While the server runs, it sees public keys, one-time signatures, `L` and ciphertext in requests and responses, and so does an observer of the unencrypted h2c hop between the provider edge and the container. A recorded request cannot be replayed, because its challenge is spent (§5.5); such an observer can still drop or delay requests.
- Someone who has a link and also steals the server (T14) can guess the passphrase offline against the ciphertext at the cost of scrypt `N = 2^17` per guess; only the strength of the passphrase protects it, which is why clients refuse passphrases shorter than 12 characters (§2).
- The provider in front of the public instance sees client addresses, request times and sizes in its own logs.
- AES-GCM proves only that whoever made the object knew `S` (and `P`). Anyone who holds the link, such as one recipient of a message with several opens or whoever intercepted the link, and who can also write to the stored ciphertext, can replace the content for the later opens with content that authenticates. Preventing it needs a sender signing key in the link (OD26, a protocol change).
- Anyone who holds the link can delete the message with `R`, also when it is protected by a passphrase, and can lock readers out with wrong passphrases (§6.4). Both cost availability, not confidentiality (D19, D40).
- Ed25519 only authorizes requests. A future quantum computer that sees `pk_R` or `pk_V` (edge, h2c hop, running server) could forge read and verifier signatures and defeat one-time enforcement on the server, but content stays protected by 256-bit symmetric keys.
