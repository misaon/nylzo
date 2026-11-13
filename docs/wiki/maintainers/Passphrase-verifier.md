# Passphrase verifier and PAKE

Finding F11, evaluated on 12 November 2026: a recorded verifier of protocol v2 together with a leaked link let its holder test passphrase guesses offline. The same day the project owner decided to replace the verifier with OPAQUE in protocol v3. This page records what the verifier exposed, the logging rules that came first, the options that were compared and how protocol v3 settles each requirement.

## What the verifier of protocol v2 exposed

- `V` and `pk_V` followed from `S`, `id` and the passphrase alone. `pk_V` travelled in `X-Nylzo-Create` at creation and in `X-Nylzo-Verifier` at every claim and burn, next to an Ed25519 signature over the request.
- Ed25519 signatures can be checked by anyone, so a recorded request plus the link was a complete test: scrypt the guess, derive `pk_V`, compare. Leaving `pk_V` out of `X-Nylzo-Verifier` would not have been enough: `X-Nylzo-Create` still carried it, and the signature confirmed a guess to anyone who also had the challenge from `Authorization`.
- Every attempt signed with a key derived from what the reader typed, so a recording also gave away wrong entries, such as a password from another service typed by mistake.
- The test cost one scrypt `N = 2^18` per guess, about 500 guesses per second on an RTX 5090 ([§13](../reference/Protocol.md#13-what-a-stolen-server-reveals)). That left the generated 80-bit passphrase out of reach. A passphrase a person chose was only as safe as its real entropy.
- The running server, the TLS proxy, the unencrypted hop between them, a provider's ingress and a TLS-inspecting proxy on the client's network all saw these headers. A log, a debug trace or a saved HAR file kept them after the message had ended.

## What F11 changed first

These rules stay in protocol v3: a logged `X-Nylzo-Lease` still downloads a blob without spending an open.

- Protocol §10 forbids every proxy, load balancer, CDN, WAF and tracing agent in front of the server from recording `Authorization`, the `X-Nylzo-*` headers of requests and responses, or the request path. §13 states the attack and who can still mount it.
- The `nylzo` Caddyfile adapter refuses a configuration that writes an access log (any site `log` directive), lets a log record `http.log`, `http.handlers` or `http.stdlib`, has no default log, or uses the `tracing` handler, the FastCGI transport or `insecure_secrets_log`. It marks every `nylzo_frontend` it checked, and `nylzo_frontend` refuses a configuration without the mark, so a Caddyfile loaded without the adapter does not start either. An operator's edit that would record these headers therefore fails at load; `TestTheAdapterRefusesRequestRecording` checks each case, and that the shipped Caddyfiles, `www` and monitor sites included, still load. Configurations already in place keep loading: the guard asks for no exclusion that the shipped Caddyfiles lack, because the Hetzner updater checks a new Caddy build against the live Caddyfile.
- `TestCaddyfilesLogNoRequestSecrets` runs the Compose Caddyfile and the Hetzner Caddyfile composed with `www.Caddyfile`, with every log sent to a file at debug level. It sends sentinel values in every `X-Nylzo-*` header and `Authorization` over HTTP/2 and HTTP/3, through a success, a claim whose response carries `X-Nylzo-Lease`, `X-Nylzo-Share` and `X-Nylzo-Policy`, an oversized body, a redirect from `www` and an upstream failure, and fails on any of these values or a message path in the log. Checked by hand, it also fails when an access log is injected past the adapter and when it runs with `GODEBUG=http2debug=2`. Caddy redacts only `Authorization`, `Cookie`, `Set-Cookie` and `Proxy-Authorization` by itself.
- The CI planner now runs the Go job, which holds the Caddy tests, for every change to a profile Caddyfile. Before, a pull request that touched only a Caddyfile skipped them. The Compose job also sends `X-Nylzo-KE3` and `X-Nylzo-Create` markers through the real container and checks its log.
- `GODEBUG=http2debug=1` or `=2` makes Go's HTTP/2 code print every header, and `QLOGDIR` makes quic-go write HTTP/3 headers to files. Every `nylzo` command that handles a secret, `serve` included, refuses to start with the first; `nylzo` links no quic-go. The `nylzo` adapter and `nylzo_frontend` refuse both, after Caddy has read `--envfile`, which can still set `QLOGDIR`.
- The server already logged none of these headers. In protocol v2 `TestStolenServerRevealsNothing` searched its log for `pk_V` and `V` in raw, base64url and hex form, which a logged `X-Nylzo-Create` would not match, because that header encodes `pk_V` inside base64url JSON. It now searches for the exact `Authorization`, `X-Nylzo-Create`, `X-Nylzo-Registration`, `X-Nylzo-KE1`, `X-Nylzo-KE3` and `X-Nylzo-Lease` values the harness sent, and for the registration responses and KE2 values it received.

What no rule here could reach: an observer that sees requests without logging them, and an instance whose operator ignores the rule. Protocol v3 closes that for the passphrase.

## Options compared

| Option | Recorded request plus link | Active attacker at the TLS proxy, at a claim | Copy of the server with its pepper, plus link | Cost |
| --- | --- | --- | --- | --- |
| Protocol v2, logging forbidden | offline test | offline test | offline test against the verifier mac while the blob lives, or against the ciphertext | none |
| Seal the verifier to an instance key (HPKE) | nothing to test | replaces the key and gets the test | as today | new header formats in both cores, an instance key to rotate, `client_outdated` for older clients |
| Passphrase hardened by an OPRF of the server (RFC 9497) | nothing to test; every guess needs a counted evaluation | gets a test by answering with its own key, unless the link commits to the server's key (VOPRF), which makes links longer | offline test | protocol v3, one more signed round trip at creation and at a claim or burn |
| OPAQUE (RFC 9807) | nothing to test; every guess needs a counted evaluation | nothing to test | offline test, inevitable for any single-server aPAKE (RFC 9807 §10.11) | protocol v3 and the largest change, listed below |
| CPace (balanced PAKE) | a recorded claim or burn: nothing to test; a recorded creation carries the password equivalent, which is an offline test and passes the check without the passphrase | nothing to test | offline test, and the record holds that password equivalent | still an Internet-Draft (draft-irtf-cfrg-cpace-21) |

An active attacker at the TLS proxy during creation defeats every option, because the sender registers through that channel; RFC 9807 §3.2 requires a server-authenticated channel for registration. The same attacker can already replace the sender's share key on the unsigned instance request ([§13](../reference/Protocol.md#13-what-a-stolen-server-reveals)), and the website takes its code from the instance anyway.

## Decision: OPAQUE in protocol v3

On 12 November 2026 the project owner approved OPAQUE (RFC 9807) with the `ristretto255-SHA512` configuration and scrypt as its stretching function, for protocol v3. It leaves both a recording and an active proxy at a claim with nothing to test, needs no longer links and stores no password equivalent; CPace matched the first two but is still a draft and fails the third. The owner also chose two of the open points: records grow to a fixed 1024 bytes and keep 8 leases, and KE2 counts as a failed attempt in the database. [Protocol](../reference/Protocol.md) §2.1, §5, §6.4 and §15 specify the result; the requirements of the evaluation were settled as follows.

- OPRF input. `password = salt_pw || pw`, so the running server, or a copy of it with its pepper, still needs the link to test a guess, as it does against the ciphertext. `salt_pw` now comes from `S` alone, because the registration request rides on the instance request, before the server issues the id.
- No dummy flow. Every message runs OPAQUE: one without a passphrase uses the empty passphrase and the identity as its stretching function. The requests, the stored record, the server's work and the counting are the same for both profiles, which a dummy flow would have to imitate. The price is small: a holder of the link can lock a message without a passphrase with unfinished handshakes, which costs no more than the burn the same link allows.
- `P` is the `export_key` of OPAQUE for both profiles, so `K` needs the server's OPRF answer besides `S` and `E`.
- Registration. `GET /api/v1/instance` carries the registration request in `X-Nylzo-Registration` and answers the registration response with the id; the 192-byte record travels in `X-Nylzo-Create`. A client of protocol v2 gets `client_outdated`.
- Binding to `erase_salt`. The record is sealed under a key from the erase salt inside the sealed record, so once the blob is erased no copy of the database tests a guess, with or without the pepper.
- Keys. The OPRF key comes from the pepper, the message id and the expiry share `E`, so it disappears with the epoch key of the message's expiry hour; the server's AKE key pair comes from the pepper and the id. The record keeps the `pepper_id`, so a rotation keeps live messages open through the previous pepper.
- Input length. The OPRF input carries a two-byte length, so both cores refuse a passphrase longer than 65,503 bytes after NFC instead of encoding it wrongly.
- Handshake. `POST /api/v1/messages/{id}/handshake` takes KE1 and answers KE2 with a challenge that holds the login state in the memory of the process that issued it. It counts one attempt in the database; a matching KE3 on the claim or burn takes it back in a transaction of its own and restores the wait that the handshake set, if no later handshake changed it. The request signature binds KE1 and KE3.
- Upgrade. The fragment and envelope versions moved to 3, the database to `user_version` 8, and the first start of `serve` erases every message of protocol v2, as the upgrade from v1 did. A v2 message has no registration and its content key does not come from OPAQUE.
- Implementation. The Go core runs OPAQUE-3DH on `github.com/gtank/ristretto255` v0.2.0 (BSD-3-Clause, by George Tankersley and Filippo Valsorda, built on `filippo.io/edwards25519`), with its own OPRF and AKE code. The web core runs the client steps on `@noble/curves` 2.4.0, which it already shipped. Both pass the ristretto255 vectors of RFC 9497 appendix A.1.1 and RFC 9807 appendix C.1.1 and the runs of both profiles in `spec/vectors.json`. Neither the Go nor the TypeScript OPAQUE code has had an independent audit.

What protocol v3 does not change:

- An active attacker at the TLS proxy while a message is created can answer the registration request with an OPRF key of its own and test guesses offline against the record it then sees; RFC 9807 §3.2 requires a server-authenticated channel for registration.
- The running server, or a copy of it with its pepper, seeds and the link before the message's expiry hour while the blob lives, tests guesses offline at one scrypt each, as every single-server aPAKE allows (RFC 9807 §10.11).
- ristretto255 is not post-quantum: a quantum computer with a recorded handshake or registration and the link could compute the OPRF key and test guesses offline.
- The time between a handshake and its claim shows the scrypt run, so a network observer can tell a message with a passphrase from one without by timing, as it could from the reader typing in protocol v2.

## Sources

- [RFC 9807: The OPAQUE Augmented PAKE Protocol](https://www.rfc-editor.org/rfc/rfc9807.html), July 2025, Informational (IRTF CFRG)
- [RFC 9497: Oblivious Pseudorandom Functions Using Prime-Order Groups](https://www.rfc-editor.org/rfc/rfc9497.html)
- [draft-irtf-cfrg-cpace](https://datatracker.ietf.org/doc/draft-irtf-cfrg-cpace/), revision 21 of April 2026
- [OWASP Logging Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html#data-to-exclude): access tokens, authentication passwords and primary secrets stay out of logs
- [Caddy `log` directive](https://caddyserver.com/docs/caddyfile/directives/log): which request headers Caddy redacts by default
- [bytemare/opaque](https://github.com/bytemare/opaque)
- [gtank/ristretto255](https://github.com/gtank/ristretto255) v0.2.0
- [RFC 9496: The ristretto255 and decaf448 Groups](https://www.rfc-editor.org/rfc/rfc9496.html)
