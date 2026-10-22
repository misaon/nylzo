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
| PBKDF2 | PBKDF2-HMAC-SHA-256 (RFC 8018) |
| HMAC | HMAC-SHA-256 (RFC 2104) |
| CSPRNG | `crypto.getRandomValues` in browsers, `crypto/rand` in Go |
| `a \|\| b` | byte concatenation |
| `u16be(n)`, `u32be(n)`, `u64be(n)` | unsigned big-endian integers of 2, 4 and 8 bytes |

No other cryptographic construction is used. The protocol has no asymmetric cryptography; flags bit 0 is reserved for a future HPKE mode.

### 1.1 base64url

All binary values carried in URLs, headers and JSON use base64url (RFC 4648 §5) **without padding** and in **canonical form**: a decoder MUST reject a string that is not the exact encoding of its decoded bytes. Concretely, the unused low bits of the final character MUST be zero, and the length MUST match the expected byte length exactly.

| Value | Bytes | Characters | Allowed final characters |
|---|---|---|---|
| `id` | 16 | 22 | `A Q g w` |
| `S`, `R`, `V`, `M`, `L`, hashes | 32 | 43 | `A E I M Q U Y c g k o s w 0 4 8` |

A canonical 43-character encoding of 32 bytes never ends in `-` or `_`, so the design's rule "regenerate `S` when its last character is `-` or `_`" holds by construction.

### 1.2 Identifiers and secrets

- `id`: 16 bytes from the client CSPRNG. Appears in the URL path and MAY appear in logs.
- `S`: 32 bytes from the client CSPRNG. Appears only in the URL fragment. The server MUST never receive `S`.
- `M`: 32 bytes from the client CSPRNG (manage token). The client sends `h(M)` at creation and `M` on cancel and status.
- `L`: 32 bytes from the server CSPRNG (lease token), issued by claim.

## 2. Key derivation (client)

Inputs: `id` (16 raw bytes), `S` (32 bytes), `kdf_id` (from the URL fragment, §4), optional passphrase.

```
hsalt   = SHA-256("nylzo/v1/hkdf-salt" || id)                            32 bytes
PRK_S   = HKDF-Extract(salt = hsalt, IKM = S)
R       = HKDF-Expand(PRK_S,  "nylzo/v1/read",     32)                  read token
salt_pw = HKDF-Expand(PRK_S,  "nylzo/v1/pw-salt",  32)                  passphrase salt
P       = kdf_id 0: empty (0 bytes)
          kdf_id 1: scrypt(pw, salt_pw, N = 2^17, r = 8, p = 1, dkLen = 32)
          kdf_id 2: PBKDF2-HMAC-SHA-256(pw, salt_pw, iterations = 600000, dkLen = 32)
PRK_SP  = HKDF-Extract(salt = hsalt, IKM = S || P)
K       = HKDF-Expand(PRK_SP, "nylzo/v1/content",  32)                  content key
V       = HKDF-Expand(PRK_SP, "nylzo/v1/verifier", 32)                  verifier
```

- `pw` is the passphrase normalised to Unicode NFC and encoded as UTF-8, for both profiles. An empty passphrase with `kdf_id` 1 or 2 is permitted by the protocol; user interfaces SHOULD warn.
- The passphrase MUST be well-formed Unicode. A core rejects invalid UTF-8 or a lone surrogate before it derives anything, so two cores never derive different keys from the same input.
- The string literals are ASCII without a terminating NUL. `info` strings are passed to HKDF-Expand verbatim.
- `S || P` has a fixed length per profile (32 or 64 bytes), so the concatenation is unambiguous.
- `K` MUST never leave the client. In the browser the client MUST derive `K` as a non-extractable `CryptoKey` (`deriveKey`, `extractable = false`) and use `deriveBits` only for `R`, `salt_pw` and `V`.
- `h(R)`, `h(V)` and `h(M)` are sent at creation; `R` and `V` are sent only at claim; `M` only at cancel and status.
- `kdf_id` for derivation MUST come from the URL fragment. A client MUST NOT derive `V` with a profile supplied by the server; when the server reports a different profile for a live message (metadata in state `available` or `opened`, or `X-Nylzo-Policy`), the client MUST stop and MUST NOT send any verifier. Tombstone metadata reports `kdf_id` 0, which is no profile; a client MUST NOT compare it.

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
- `kdf_id` MUST be 0, 1 or 2 and MUST equal the profile in the URL fragment.
- `flags`: bit 0 is reserved for the HPKE mode and MUST be 0 in v1; all other bits MUST be 0. A decoder MUST reject non-zero flags. No flag reveals whether a message carries a file.
- `iv`: 12 bytes from the CSPRNG, fresh per message. One `K` encrypts exactly one envelope.
- `opens_max` and `ttl_s` are the sender's policy. The recipient obtains them from `X-Nylzo-Policy` (§6.4) and the metadata response and decrypts with them; any change by the server or an intermediary fails authentication.
- `len_meta` is the byte length of `meta`.
- `file_bytes` are present exactly when `meta.file` is present and have length `meta.file.size`.
- `padding` is zero bytes. The plaintext length (`4 + len_meta + len(file_bytes) + len(padding)`) MUST be a multiple of 4096 bytes; when `meta.file` is present it MUST be a multiple of 65536 bytes. The decoder MUST derive the content length from `len_meta` and `meta.file.size`, MUST NOT depend on the padding content, and MAY ignore it.
- Limits: `meta.text` at most 102400 bytes of UTF-8; `meta.file.size` at most 5242880; `len_meta` at most 104448; `object` at most 5410880 bytes (5 MiB + 100 KiB + 64 KiB + 64 B). These are protocol maxima that both reference cores enforce. An instance MAY report lower limits in `/api/v1/instance` and MUST NOT report higher ones. An instance derives `max_object_bytes` from its file limit as the largest object a valid meta and file can produce: `min(5410880, 15 + 16 + pad65536(4 + 104448 + max_file_bytes))`, where `pad65536` rounds up to a multiple of 65536. With the default file limit that is 5373983.

### 3.1 meta **[detail]**

`meta` is a JSON object:

```json
{"v":1,"text":"…","file":{"name":"…","mime":"…","size":N}}
```

- `v` MUST be 1. `text` and `file` are each optional; at least one MUST be present.
- `file.name`: 1 to 255 bytes of UTF-8, no `/`, `\`, C0 control characters (U+0000 to U+001F) or DEL (U+007F). Clients MUST additionally sanitise the name before display and download.
- `file.mime`: 0 to 255 bytes. `file.size`: non-negative integer equal to the length of `file_bytes`.
- Encoders MUST produce the canonical form so both cores emit identical bytes: UTF-8, no whitespace, keys in the order `v`, `text`, `file` (and `name`, `mime`, `size` inside `file`), absent keys omitted, integers in decimal without sign, exponent or fraction. String escaping: `"` → `\"`, `\` → `\\`, U+0008 → `\b`, U+0009 → `\t`, U+000A → `\n`, U+000C → `\f`, U+000D → `\r`, every other code point below U+0020 → `\u00XX` with lowercase hex, every other code point literal UTF-8 (including `/`, U+2028 and U+2029).
- Decoders MUST accept any RFC 8259 JSON object that satisfies the constraints above, MUST ignore unknown keys, and take the last occurrence of a duplicated key. They MUST reject meta nested deeper than 32 levels; the top-level object is level 1 and brackets inside strings do not count.

## 4. Link

```
https://{host}/m/{id}#1.{kdf_id}.{S}
```

- Path `/m/{id}` with `id` in canonical base64url (22 characters). The server serves a static page for it; loading it consumes nothing.
- Fragment: fragment version `1`, then `kdf_id` (`0`, `1` or `2`), then `S` in canonical base64url (43 characters), separated by `.`. The fragment never leaves the browser in any request.
- A client MUST reject an unknown fragment version, an unknown `kdf_id`, or a non-canonical `S`.
- A client parses a link without URL normalisation. The text before the first `#` MUST match `scheme "://" authority path ["?" query]`: `scheme` is `http` or `https` in any letter case, `authority` contains only ASCII letters, digits, `.`, `-`, `:`, `[` and `]`, `path` is exactly `/m/{id}`, and the query is ignored. The fragment is everything after the first `#`, so a link with `##` carries the unknown fragment version `#1` and is invalid. In the scheme, authority and path, relative references, dot segments, backslashes, userinfo, percent-encoding and leading or trailing whitespace are therefore rejected; the query may contain any character except `#`. Both reference cores accept the same set of links.

## 5. Server-side tokens and secrets

### 5.1 Pepper and purpose keys

- `pepper`: 32 bytes of configuration (`NYLZO_PEPPER`), never stored in the database.
- `k_purpose = HKDF-SHA-256(salt = empty, IKM = pepper, info = "nylzo/v1/mac/" || purpose, 32)` for `purpose` in `read`, `verifier`, `manage`, `lease`, `ip`, `pow`.
- `mac_purpose(x) = HMAC-SHA-256(k_purpose, x)`.
- `pepper_id = SHA-256("nylzo/v1/pepper-id" || pepper)[0]` (one byte). Every row that stores a mac carries the `pepper_id` of the key that produced it.
- Rotation: `NYLZO_PEPPER_PREVIOUS` holds the previous pepper. The server writes with the current pepper and verifies each row with the pepper whose `pepper_id` matches the row; a row whose `pepper_id` matches neither verifies as invalid (with the same work as a valid row). The server MUST refuse to start when both peppers share a `pepper_id`. The maximum TTL is 7 days, so no rewriting is needed: after 7 days no live message carries the previous `pepper_id`. Tombstones keep it for another 7 days; dropping the previous pepper earlier turns those tombstones into unknown ids (404) without affecting any live message. An expired message becomes a tombstone when the next request or sweep ends it, so keeping the previous pepper for 14 days plus one sweep interval preserves every tombstone state. A rotation also changes `k_ip` and with it every address bucket (§5.3).
- `sweep-once` does not need a pepper.

### 5.2 What the server stores

| Client sends | Server stores |
|---|---|
| `read_hash = h(R)` at creation | `read_mac = mac_read(h(R))` |
| `verifier_hash = h(V)` at creation (passphrase only) | `verifier_mac = mac_verifier(h(V))` |
| `manage_hash = h(M)` at creation | `manage_mac = mac_manage(h(M))` |
| `R` at metadata, claim, blob, ack, burn, abuse | server computes `mac_read(h(R))` and compares |
| `V` at claim | server computes `mac_verifier(h(V))` and compares |
| `M` at cancel, status | server computes `mac_manage(h(M))` and compares |
| (server issues `L` at claim) | `lease_mac = mac_lease(L)` |

All comparisons MUST be constant-time. When a row does not exist, the server MUST still compute a mac and compare it against a dummy value so that the work and the response are identical.

Tombstones round `first_opened_at` and `ended_at` down to the minute, and `purge_at` is `ended_at + 7 d` from the rounded value. An ending finished at or after `expires_at` records `expires_at` as its end, so a tombstone never reveals when someone touched an expired link. A lease row keeps `lease_expires_at` to the millisecond, so while it exists (the 120 s lease, until the ack or the next sweep) it reveals the claim time that precisely; this is accepted.

Deleted rows leave no readable trace in the store. SQLite runs with `secure_delete` and `journal_size_limit = 0`, every sweep ends with `wal_checkpoint(TRUNCATE)`, and a database created by an older build is vacuumed once when it is first opened. `tombstones`, `passphrase_attempts` and `rate_counters` are tables without rowid, so their row order does not reveal the order of events. `passphrase_attempts.next_attempt_at` is 0 while no wait applies and is otherwise rounded up to the next full minute. Reading a blob does not update the access time of its file (`O_NOATIME` on Linux). What the file system or the storage provider keeps of deleted data is outside the server's control.

### 5.3 Address buckets

Addresses never reach a persistent log. For rate limits and passphrase backoff the server derives a 24-bit bucket:

```
addr      = canonical IP (netip.ParseAddr; IPv4-mapped IPv6 unmapped to IPv4)
prefix    = addr masked to /32 or /24 (IPv4), /64 or /48 (IPv6)
input     = date_utc ("YYYY-MM-DD", ASCII) || 0x00 || family (0x04 or 0x06) || prefix_len (1 byte) || masked address bytes (4 or 16)
bucket    = HMAC-SHA-256(k_ip, input)[0:3]
```

The date rotates at 00:00 UTC; all instances share the derivation because they share the pepper. Backoff state keyed on a bucket resets at that rotation (accepted). `k_ip` comes from the current pepper only, so a pepper rotation resets every bucket in the same way: the rate counters, the passphrase backoff and the abuse cap per range start again. This is accepted too, because the daily cap of 100 failures per message still holds. With `NYLZO_PASSPHRASE_IP_SCOPE=off` the passphrase backoff uses a constant bucket (per message only).

## 6. HTTP API

Base path `/api/v1`. Request and response bodies are JSON (`application/json; charset=utf-8`) except where noted. Timestamps are RFC 3339 in UTC. Binary values use canonical base64url (§1.1). `Authorization: Bearer {token}` carries `R` or `M` as 43 base64url characters.

| Method and path | Auth | Consumes an open |
|---|---|---|
| `POST /api/v1/messages` | none (rate limit, PoW under load) | no |
| `GET /api/v1/messages/{id}` | Bearer R | no |
| `POST /api/v1/messages/{id}/claim` | Bearer R (+ verifier) | yes |
| `GET /api/v1/messages/{id}/blob` | Bearer R + `X-Nylzo-Lease` | no |
| `POST /api/v1/messages/{id}/ack` | Bearer R + `X-Nylzo-Lease` | no |
| `POST /api/v1/messages/{id}/burn` | Bearer R | ends the message |
| `POST /api/v1/messages/{id}/cancel` | Bearer M | ends the message |
| `GET /api/v1/messages/{id}/status` | Bearer M | no |
| `GET /api/v1/instance` | none | no |
| `POST /api/v1/abuse` | none (rate limit; needs R in the body) | no |

No endpoint accepts or returns plaintext, `S`, `K` or a passphrase.

### 6.1 Errors

| Status | Body | When |
|---|---|---|
| 400 | `{"error":"bad_request"}` | malformed `X-Nylzo-Create` (including the id inside it) or request body, policy outside instance limits, `size` ≠ Content-Length, version or kdf mismatch, an abuse report without `Content-Type: application/json` |
| 403 | `{"error":"passphrase","retry_after":N,"attempts_left":N}` | wrong verifier or backoff active; no open consumed |
| 403 | `{"error":"pow"}` | PoW required and missing, invalid or already used |
| 404 | `{"error":"not_found"}` | unknown id, wrong R, wrong M, and a malformed id or token in the path or `Authorization` (indistinguishable) |
| 409 | `{"error":"conflict"}` | id exists in `messages` or `tombstones` (identical for both) |
| 410 | `{"error":"gone","state":"opened\|expired\|cancelled\|deleted"}` | message ended |
| 410 | `{"error":"gone","state":"available\|opened"}` | blob or ack with a lease that is not active while the message is still live; `state` is the message's current state, read again after the lease check, so a message that ended meanwhile answers its end state |
| 411 | `{"error":"length_required"}` | create without Content-Length |
| 413 | `{"error":"too_large"}` | Content-Length above the instance limit |
| 429 | `{"error":"rate_limited","retry_after":N}` + `Retry-After` header | bucket limit, source, prefix or instance token bucket, 4th blob download in a lease |
| 500 | `{"error":"internal"}` | storage or blob failure; the body never carries details. A request whose client went away answers 400 instead. A claim that fails after its transaction committed still carries `X-Nylzo-Lease`, so the client can retry through `/blob`. A live, unexpired message whose blob is missing without a recorded ending (§7) answers 500 and stays live; a claim checks this before it spends an open |
| 503 | `{"error":"unavailable"}` + `Retry-After` | global creation cap, `Sec-Purpose` requests, PoW verifier saturated, abuse report file full for a report that would be stored |

The 404 response (status, headers, body bytes) MUST be byte-identical for an unknown id, a wrong `R` and a wrong `M`. The 409 response MUST be identical for a live id and a tombstone, and for a conflict found before the body is read or while storing it.

### 6.2 Create

```
POST /api/v1/messages
Content-Type: application/octet-stream
Content-Length: {size}
X-Nylzo-Create: base64url(JSON {id, read_hash, verifier_hash?, manage_hash, kdf_id, opens_max, ttl_s, size})
X-Nylzo-Pow: {token}            only when the instance reports pow.required
body: object (§3)
```

Server order of checks, each before reading the body: (1) parse the header and the id syntax; (2) `Content-Length` present (411), equal to `size` (400), below the limit (413); (3) atomic per-bucket rate counters, counting the attempt even if it fails later (429 when over); (4) the global daily cap: 503 when the counter has reached the cap, PoW when it is at or above 50 % (403, §8.1), then one conditional increment reserves a slot (503 when none is left by then). A request that reserved a slot at or above 50 % without a verified PoW returns the slot and answers 403 `pow`; (5) id absent from `messages` and `tombstones` (409, same for both). The body may take `max(120 s, size / 16000 s)` to arrive, which is 128 kbit/s for the whole object. The server then streams the body through a size-bounded reader, reads the 15-byte header, stores `version` from it and rejects (400) a `kdf_id` that differs from the header field, stores the blob and the row with `created_at` and `expires_at = created_at + ttl_s` taken when the upload has been stored, so a slow upload does not use up the TTL, and answers:

```
201 {"id":"…","expires_at":"…"}
```

- `verifier_hash` MUST be present exactly when `kdf_id ≠ 0`.
- `opens_max` MUST be between 1 and the instance maximum; `ttl_s` between 60 and the instance maximum. The server MUST NOT clamp a policy; it rejects with 400.
- The header JSON MUST NOT contain unknown keys.
- Only stored messages keep their slot in the global cap. Every failure after the reservation, including a conflict, a bad body, a storage error and a client that disconnects, returns the slot. Concurrent creates therefore never exceed the cap; a process that crashes while it holds a slot leaves it taken until the UTC day ends. While uploads are in flight the counter includes their slots. A second counter holds stored messages only: when it has reached the cap, the 503 carries a `Retry-After` until the next UTC day, otherwise 60 s. The 60 s hint therefore also covers slots that a crashed process left taken, and it can repeat until the day ends.

### 6.3 Metadata (non-consuming)

```
GET /api/v1/messages/{id}
Authorization: Bearer {R}
200 {"state":"available|opened|expired|cancelled|deleted","created_at":"…","expires_at":"…","opens_max":N,"opens_remaining":N,"ttl_s":N,"passphrase_required":bool,"kdf_id":N,"size":N,"first_opened_at":"…"?,"ended_at":"…"?}
404 for unknown id and for a wrong R
```

Metadata never changes `opens_remaining` or the state. It MAY complete the deletion of a blob that is already unreachable (expired message, or `opens_remaining == 0` with no active lease), and it finishes an ending recorded in `messages.ending` (§7). Tombstones answer 200 with their state, `first_opened_at` (rounded to the minute, absent when the message was never opened) and `ended_at`. A tombstone keeps no policy or times, so `created_at` and `expires_at` are absent, `opens_max`, `opens_remaining`, `ttl_s`, `kdf_id` and `size` are 0 and `passphrase_required` is false. `kdf_id` 0 on a tombstone is not a profile (§2). While an ending is recorded but not yet finished (§7), metadata and status already report the ended state but keep the live fields and carry no `ended_at`; they switch to the tombstone shape once the ending is finished.

### 6.4 Claim

```
POST /api/v1/messages/{id}/claim
Authorization: Bearer {R}
Content-Type: application/json
body: {"verifier":"{V}"}          only when passphrase_required
200
  X-Nylzo-Lease: {L}
  X-Nylzo-Policy: base64url(JSON {"opens_max":N,"ttl_s":N,"kdf_id":N,"flags":N})      [detail]
  Content-Type: application/octet-stream
  body: object
403 {"error":"passphrase","retry_after":N,"attempts_left":N}
410 {"error":"gone","state":"…"}
```

After it has read and checked the request body, and before the transaction, the server opens the blob and keeps it open for the response. A blob whose size differs from the stored `size` answers 500 and spends no open. A missing blob on a message that has ended (a recorded ending, or `expires_at ≤ now`) answers 410 with the end state and finishes the ending; on any other message it answers 500 and spends no open.

In one storage transaction:

1. Read `now` from the process clock, after the transaction holds the write lock, and load the row. A recorded ending, `state ≠ available` or `expires_at ≤ now` → 410.
2. With a passphrase: if the message's daily cap (100 failures per UTC day) is reached, or `next_attempt_at` for `(id, bucket)` is in the future → 403 with `retry_after` (seconds; until the next UTC midnight for the daily cap) and `attempts_left`. Otherwise compare `mac_verifier(h(V))` in constant time. On mismatch: `fail_count(id, bucket) += 1`, `day_fail_count(id) += 1`, `fail_count_total(id) += 1`; when `fail_count ≤ 5` answer 403 with `retry_after = 0` and `attempts_left = 5 − fail_count`; from the 6th failure set `next_attempt_at` to `now + min(2^(fail_count − 5), 3600)` seconds rounded up to the next full minute, and answer 403 with the resulting `retry_after` and `attempts_left = 0`. No open is consumed. A claim without a verifier on a message with a passphrase counts as a mismatch.
3. `opens_remaining −= 1`; insert a lease `(id, mac_lease(L), lease_expires_at = now + max(120 s, size / 16000 s), downloads = 0)`. When `opens_remaining == 0` set `state = opened`.
4. Return the object with the lease and the policy.

A lease is active while `now < lease_expires_at`. The instance that served the claim schedules `ReleaseIfDone(id)` at `lease_expires_at + 1 s`. A claim or blob response may take as long as its lease: the write deadline is `max(120 s, size / 16000 s)` from the start of the response, so a recipient on a 128 kbit/s link still receives the largest object.

Definition of one open: one successful atomic release of the envelope by the server after proof of the link (and the passphrase). Loading the page, metadata, a decryption attempt or a wrong passphrase never count.

### 6.5 Blob, ack

```
GET  /api/v1/messages/{id}/blob    Authorization: Bearer {R}, X-Nylzo-Lease: {L}
  200 object (also X-Nylzo-Policy), 410 when the lease is not active (§6.1) or the message ended, 429 from the 4th download in one lease
POST /api/v1/messages/{id}/ack     Authorization: Bearer {R}, X-Nylzo-Lease: {L}
  204; ends the lease early and calls ReleaseIfDone
```

Blob downloads count in the read rate limit and are capped at 3 per lease. The blob is deleted as soon as `opens_remaining == 0` and no lease is active. A claim or download that finds the blob already deleted by a concurrent ending, including an expiry, answers 410 without `X-Nylzo-Lease` and with the end state (§7), finishing that ending first if it was interrupted. A missing blob on an unexpired message without a recorded ending is a storage fault: the server answers 500, keeps the row and logs an error. A download that finds an ending already recorded answers 410 with that state and finishes the ending. `HEAD` on `/blob` answers 404 like any other unsupported method, so it never spends a download. A stream that ends early is logged as `blob-stream`. The server finds a lease by message id, lease row and `lease_mac` together, so a stale lease token never touches a lease of another claim, even when the store reuses row numbers. An ack that finds an ending recorded answers 410 with that state and finishes the ending, like a download.

### 6.6 Burn, cancel, status, instance, abuse

```
POST /api/v1/messages/{id}/burn     Bearer R → 204; state = deleted, blob deleted
POST /api/v1/messages/{id}/cancel   Bearer M → 204; state = cancelled, blob deleted
GET  /api/v1/messages/{id}/status   Bearer M → 200 {"state":"…","opens_used":N,"opens_max":N,"created_at":"…","expires_at":"…","first_opened_at":"…"?,"ended_at":"…"?,"failed_passphrase_attempts":N}
GET  /api/v1/instance               → 200 {"limits":{"max_file_bytes":N,"max_text_bytes":N,"max_object_bytes":N,"max_ttl_s":N,"default_ttl_s":N,"max_opens":N,"default_kdf_id":N},"pow":{"required":bool,"difficulty":N,"challenge":"…"?},"versions":{"api":1,"envelope":1,"fragment":1,"server":"…"}}
POST /api/v1/abuse                  {"id":"…","read_token":"{R}","reason":"…"?} → 202 {"status":"accepted"} for known, unknown and malformed ids alike; a malformed id is answered before any rate counter
```

- `failed_passphrase_attempts` is cumulative over the message lifetime (`fail_count_total`), not per day.
- After the message ends, status reads the tombstone: only `state`, `first_opened_at` and `ended_at` are meaningful; `created_at` and `expires_at` are absent and the counters are 0.
- `first_opened_at` and `ended_at` are rounded down to the minute.
- A wrong `M` on cancel or status answers the same 404 as an unknown id.
- Abuse reports are rate limited to 5 per hour per bucket, reviewed by a human, and never delete anything automatically.
- An abuse report MUST carry `Content-Type: application/json`, so a cross-site page needs a CORS preflight, which the server never grants. Before storing `reason`, the server percent-decodes it up to three times, replaces control characters with spaces, drops format characters such as bidi controls, replaces every `#…` run and every run of 22 or more base64url characters with `[redacted]`, then cuts it to 1000 bytes and further until its JSON encoding fits in 1200 bytes. It stores at most 3 reports per message and reporter range (the /24 or /48 bucket, §5.3) per UTC day and answers 202 for the rest, so one range cannot use up the budget of others. The server reserves the count before it writes a report and returns it when the write fails. A valid report that would be stored but would push the report file past 16 MiB answers 503, and the server logs the error `abuse-file-full` for the operator. A stored report holds the UTC date, the id and the redacted reason, never the time of day. Reports older than 14 days (the 7-day maximum TTL plus the 7-day tombstone) are dropped by every sweep of the server and before every new report.

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
- Ending a message takes three steps. (1) One transaction records the intended state in `messages.ending`; the first recorded ending wins, so a later burn, cancel or expiry finishes the recorded one and reports its state. `ReleaseIfDone` records `opened` when it hands out the blob of an unexpired message after the last lease. An expired row records nothing, because `expires_at ≤ now` already ends it durably; it ends as `opened` when no opens remain and as `expired` otherwise. (2) `Blobs.Delete`. (3) One transaction deletes the `messages` row and its `leases` and `passphrase_attempts` and inserts the tombstone with the recorded state. A failed delete leaves the message alive with its ending recorded; claims, downloads, acks, burns and cancels answer 410 with that state, metadata and status report it, and every later request on the id and every sweep finishes the ending. A failure to finish is logged and never changes the response to a wrong token, which stays the uniform 404. Once burn or cancel has recorded its ending, the server finishes it even if the client disconnects. A burn or cancel that recorded the first ending answers 204 even when a concurrent release finishes that ending before it does.
- `ReleaseIfDone(id)` is idempotent: when `opens_remaining == 0`, no lease is active and `blob_ref` is set, it returns `blob_ref`, and keeps returning it until the caller, after a successful `Blobs.Delete`, clears it with `ClearBlob(id, blob_ref)`. `Blobs.Delete` is idempotent and treats a missing object as success. Callers: ack, the lease-end timer, every request on the id, and the sweep.
- Sweep (built-in goroutine every `NYLZO_SWEEP_INTERVAL`, default 5 min; or `nylzod sweep-once` from an external scheduler) ends expired messages, deletes expired leases, calls `ReleaseIfDone` for every message with `opens_remaining == 0` and a blob, finishes every recorded ending that a crash interrupted, purges tombstones after `purge_at`, purges expired `rate_counters` and `passphrase_attempts`, in batches of 1000 rows paged by key, so rows that keep failing never hide the rows after them. Every sweep also deletes temporary upload files older than 15 minutes. Once per UTC day the sweep lists the blob store and deletes objects older than 15 minutes whose ref no row holds. A counter in `rate_counters` records the day once a reconcile has listed both the rows and the blob store, so later sweeps that day, including separate `sweep-once` runs, skip it. A reconcile whose listing failed runs again on the next sweep; objects that failed to delete wait for the next day. The built-in sweeper also runs once at startup. Two sweeps that overlap may both reconcile, which is harmless. One failing item never stops the remaining steps, and `sweep-once` exits non-zero when any step failed.
- Clocks: every comparison of times (expiry, leases, backoff, counter windows, PoW windows) uses the clock of the process that handles the request or runs the sweep. All instances and sweep jobs MUST run with synchronised clocks; the reference store does not read a clock of its own. A claim passes its clock to the store, which reads it after the transaction holds the write lock, so a claim that waited for the writer never starts a lease after `expires_at`.
- Reuse of an ended id: once the tombstone is purged, the id is unknown again and a new create may use it. Anyone who still holds the old link knows `S` and could publish new content under the same link. This is an accepted risk: the server keeps no trace of a message longer than 7 days after it ends.
- Guarantees: access ends immediately (every request checks state, expiry and leases). Active data is deleted immediately on burn, cancel and the ack of the last lease; after the last lease expires without ack, at the end of the lease on a best-effort timer and at the latest by the next sweep.

## 8. Rate limits, caps, proof of work

- In-memory token buckets in front of the database for `/api/*`, checked in order: one per source, keyed by the /32 or /64 address bucket (§5.3; `NYLZO_RATE_SOURCE_RPS` default 10 requests per second, `NYLZO_RATE_SOURCE_BURST` default 20); one per prefix, keyed by the /24 or /48 bucket, with half the instance rate and burst; and one per instance (50 requests per second, burst 100). A request that an earlier bucket refuses takes no token from the later ones, so one /64 or /32 source, or one /48 or /24 range, cannot take more than half of the instance bucket. Many sources from different ranges can still drain it; the design does not claim availability against them. Each per-source and per-prefix table tracks at most 65536 entries; when full it drops entries that have refilled, then a random eighth. `/m/*` never queries the database and is not throttled. The id syntax is checked before any query.
- Database counters per bucket (§5.3), incremented in one transaction by atomic statements with `RETURNING`, windows by the server clock. The /32 or /64 counters come first. A request over one limit is not counted against the limits after it, so a refused source does not use up its /24 or /48 budget. A refused request gets a `Retry-After` of at least 1 s, even when its window ends while the counters are written:

| Limit | /32 or /64 | /24 or /48 |
|---|---|---|
| create per hour | 20 | 100 |
| create per day | 100 | 500 |
| create bytes per day | 50 MB | 250 MB |
| metadata + claim + blob per hour | 300 | 1500 |
| abuse reports per hour | 5 | 25 |

- Global daily creation cap (`NYLZO_GLOBAL_DAILY_CREATES`, 0 = off): it counts stored messages plus creates still uploading (§6.2). From 50 % of the cap PoW is required for creation; at 100 % creation answers 503 while reading continues. `/api/v1/instance` reads the shared counter, so every instance reports the same state.
- Counter rows expire one hour after an hourly window and 24 hours after a daily window; a PoW nonce row expires 240 s after its challenge was issued and the daily reconcile marker 24 hours after its day. The sweep deletes them in batches of 1000.

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

## 9. Transport and headers

- `http.Server` timeouts: `ReadHeaderTimeout` 5 s, `ReadTimeout` 120 s, `WriteTimeout` 120 s, `IdleTimeout` 30 s, `MaxHeaderBytes` 8 KiB. A create body and a claim or blob response get `max(120 s, size / 16000 s)` instead (§6.2, §6.4). Unencrypted HTTP/2 (h2c) buffers at most 64 KiB of unread body per stream and 1 MiB per connection, so a create that has not started reading its body cannot hold back the other streams on its connection. Bodies are bounded by a size-limited reader at the instance object limit. The server never decompresses request bodies.
- Client address: taken from the connection. `X-Forwarded-For` is honoured only when the connection comes from `NYLZO_TRUSTED_PROXIES` (CIDR list), taking the rightmost value that is not a trusted proxy. `NYLZO_PROXY_MODE=scaleway` reads `X-Real-IP` instead. Without configuration both headers are ignored.
- Every response carries: `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `X-Robots-Tag: noindex, nofollow`, `X-Content-Type-Options: nosniff`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin`, `Strict-Transport-Security: max-age=31536000; includeSubDomains`, `Permissions-Policy: accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()`.
- HTML responses additionally carry `Content-Security-Policy: default-src 'none'; script-src 'nonce-{nonce}'; style-src 'nonce-{nonce}'; connect-src 'self'; img-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; require-trusted-types-for 'script'` with a fresh nonce of at least 128 bits per response.
- Requests with a `Sec-Purpose` header to `/m/*` or `/api/*` answer 503. Known link-preview user agents receive, for `/m/*`, minimal HTML without script.

## 10. Logging

The server logs time, method, route pattern, status, latency and response size. It never logs a message id, so a log line cannot be joined with a message even by someone who holds the database and the pepper. It never logs the raw path, so a mangled link whose fragment reached the path is not logged. Requests that a middleware answers before routing (token-bucket 429s and `Sec-Purpose` 503s) are logged without route. Internal `net/http` errors are logged as a fixed message with a category (`accept`, `panic`, `tls-handshake`, `http2`, `superfluous-write-header`, `other`; HTTP/2 handler panics count as `panic`) and without their text, because that text can carry a peer address. Warning and error categories are rate limited separately, each to a burst of 20 and 1 per second, and the next logged line reports how many were dropped. A failed sweep logs its report, the number of failed items and the first error. It MUST NOT log `Authorization`, `X-Nylzo-Create`, `X-Nylzo-Lease`, `X-Nylzo-Pow`, request or response bodies, query strings, or client addresses. Addresses exist only inside bucket hashes in `rate_counters` and `passphrase_attempts`.

## 11. Versioning

Envelope version in the first byte, fragment version before the first dot, API under `/api/v1`, `kdf_id` in the envelope header and the fragment. A client MUST reject an unknown version or non-zero flags. A new KDF profile or the HPKE mode is a new value in an existing field, not a new format.

## 12. Data model (reference)

```
messages(id TEXT PK, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining,
         pepper_id, read_mac BLOB, manage_mac BLOB, verifier_mac BLOB NULL, size, blob_ref TEXT NULL,
         day_fail_count, day_fail_date, fail_count_total, first_opened_at NULL, ending TEXT NULL)
leases(row_id INTEGER PK, id FK, lease_mac BLOB, pepper_id, lease_expires_at, downloads)
passphrase_attempts(id, bucket BLOB(3), fail_count, next_attempt_at, purge_at)   WITHOUT ROWID
tombstones(id TEXT PK, pepper_id, read_mac, manage_mac, state, first_opened_at NULL, ended_at, purge_at)   WITHOUT ROWID
rate_counters(key TEXT, window_start, count, bytes, purge_at)   WITHOUT ROWID
```

Blob store: one object per message. `blob_ref` is the message id. On disk the file name is the lowercase hex of the ref bytes (`data/blobs/{hex(ref)}`), so case-insensitive file systems cannot merge two ids; on S3 the object key is the ref. Publishing an object never replaces an existing one: a second create with the same id answers 409 and leaves the first object intact.
