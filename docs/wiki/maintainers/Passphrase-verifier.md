# Passphrase verifier and PAKE

Finding F11, evaluated on 12 November 2026: a recorded verifier together with a leaked link lets its holder test passphrase guesses offline. This page records what the verifier exposes, what changed and which protocol change would close the gap.

## What the verifier exposes

- `V` and `pk_V` follow from `S`, `id` and the passphrase alone ([protocol §2](../reference/Protocol.md#2-key-derivation-client)). `pk_V` travels in `X-Nylzo-Create` at creation and in `X-Nylzo-Verifier` at every claim and burn, next to an Ed25519 signature over the request.
- Ed25519 signatures can be checked by anyone, so a recorded request plus the link is a complete test: scrypt the guess, derive `pk_V`, compare. Leaving `pk_V` out of `X-Nylzo-Verifier` would not be enough: `X-Nylzo-Create` still carries it, and the signature confirms a guess to anyone who also has the challenge from `Authorization`.
- Every attempt signs with a key derived from what the reader typed, so a recording also gives away wrong entries, such as a password from another service typed by mistake.
- The test costs one scrypt `N = 2^18` per guess, about 500 guesses per second on an RTX 5090 ([§13](../reference/Protocol.md#13-what-a-stolen-server-reveals)). That leaves the generated 80-bit passphrase out of reach. A passphrase a person chose is only as safe as its real entropy.
- The running server, the TLS proxy, the unencrypted hop between them, a provider's ingress and a TLS-inspecting proxy on the client's network all see these headers. A log, a debug trace or a saved HAR file keeps them after the message has ended.

## What F11 changed

- Protocol §10 forbids every proxy, load balancer, CDN, WAF and tracing agent in front of the server from recording `Authorization`, the `X-Nylzo-*` headers of requests and responses, or the request path. §13 states the attack and who can still mount it.
- The `nylzo` Caddyfile adapter refuses a configuration that writes an access log (any site `log` directive), lets a log record `http.log`, `http.handlers` or `http.stdlib`, has no default log, or uses the `tracing` handler, the FastCGI transport or `insecure_secrets_log`. It marks every `nylzo_frontend` it checked, and `nylzo_frontend` refuses a configuration without the mark, so a Caddyfile loaded without the adapter does not start either. An operator's edit that would record these headers therefore fails at load; `TestTheAdapterRefusesRequestRecording` checks each case, and that the shipped Caddyfiles, `www` and monitor sites included, still load. Configurations already in place keep loading: the guard asks for no exclusion that the shipped Caddyfiles lack, because the Hetzner updater checks a new Caddy build against the live Caddyfile.
- `TestCaddyfilesLogNoRequestSecrets` runs the Compose Caddyfile and the Hetzner Caddyfile composed with `www.Caddyfile`, with every log sent to a file at debug level. It sends sentinel values in every `X-Nylzo-*` header and `Authorization` over HTTP/2 and HTTP/3, through a success, a claim whose response carries `X-Nylzo-Lease`, `X-Nylzo-Share` and `X-Nylzo-Policy`, an oversized body, a redirect from `www` and an upstream failure, and fails on any of these values or a message path in the log. Checked by hand, it also fails when an access log is injected past the adapter and when it runs with `GODEBUG=http2debug=2`. Caddy redacts only `Authorization`, `Cookie`, `Set-Cookie` and `Proxy-Authorization` by itself.
- The CI planner now runs the Go job, which holds the Caddy tests, for every change to a profile Caddyfile. Before, a pull request that touched only a Caddyfile skipped them. The Compose job also sends `X-Nylzo-Verifier` and `X-Nylzo-Create` markers through the real container and checks its log.
- `GODEBUG=http2debug=1` or `=2` makes Go's HTTP/2 code print every header, and `QLOGDIR` makes quic-go write HTTP/3 headers to files. Every `nylzo` command that handles a secret, `serve` included, refuses to start with the first; `nylzo` links no quic-go. The `nylzo` adapter and `nylzo_frontend` refuse both, after Caddy has read `--envfile`, which can still set `QLOGDIR`.
- The server already logged none of these headers. `TestStolenServerRevealsNothing` searched its log for `pk_V` and `V` in raw, base64url and hex form, which a logged `X-Nylzo-Create` would not match, because that header encodes `pk_V` inside base64url JSON. It now also searches for the exact `Authorization`, `X-Nylzo-Create`, `X-Nylzo-Verifier` and `X-Nylzo-Lease` values the harness sent.

What no rule here can reach: an observer that sees requests without logging them, and an instance whose operator ignores the rule.

## Options for a stronger model

| Option | Recorded request plus link | Active attacker at the TLS proxy, at a claim | Copy of the server with its pepper, plus link | Cost |
| --- | --- | --- | --- | --- |
| Today, logging forbidden | offline test | offline test | offline test against the verifier mac while the blob lives, or against the ciphertext | none |
| Seal the verifier to an instance key (HPKE) | nothing to test | replaces the key and gets the test | as today | new header formats in both cores, an instance key to rotate, `client_outdated` for older clients |
| Passphrase hardened by an OPRF of the server (RFC 9497) | nothing to test; every guess needs a counted evaluation | gets a test by answering with its own key, unless the link commits to the server's key (VOPRF), which makes links longer | offline test | protocol v3, one more signed round trip at creation and at a claim or burn |
| OPAQUE (RFC 9807) | nothing to test; every guess needs a counted evaluation | nothing to test | offline test, inevitable for any single-server aPAKE (RFC 9807 §10.11) | protocol v3 and the largest change, listed below |
| CPace (balanced PAKE) | a recorded claim or burn: nothing to test; a recorded creation carries the password equivalent, which is an offline test and passes the check without the passphrase | nothing to test | offline test, and the record holds that password equivalent | still an Internet-Draft (draft-irtf-cfrg-cpace-21) |

An active attacker at the TLS proxy during creation defeats every option, because the sender registers through that channel; RFC 9807 §3.2 requires a server-authenticated channel for registration. The same attacker can already replace a reader's share key ([§13](../reference/Protocol.md#13-what-a-stolen-server-reveals)), and the website takes its code from the instance anyway.

## Recommendation

Adopt OPAQUE with the `ristretto255-SHA512` configuration and scrypt as its key stretching function in protocol v3, and use its `export_key` in place of `P` when deriving `K`. It leaves both a recording and an active proxy at a claim with nothing to test, needs no longer links, and stores no password equivalent. CPace matches the first two but is still a draft and fails the third.

What it would take:

- The OPRF input has to carry material from the link, for example `salt_pw || pw`. OPAQUE's stretching step takes no salt, so with the bare passphrase as input the running server, or a copy of the server with its pepper together with one recorded request of the message, could test guesses without the link. With the link bound in, both need the link, as today, and a copy of the blob needs the pepper as well as the link.
- Creation would run OPAQUE registration before the create request. Today every create, claim and burn carries a verifier, also without a passphrase, so the headers do not show which messages have one (§2); the registration and the KE1/KE2 round trip would need a dummy run for `kdf_id` 0 to keep that.
- The registration record is 192 bytes. In place of the 32-byte verifier mac it makes a record with 8 leases 647 bytes, past the 512 bytes that every record has today (§5.2): every record would grow to 1024 bytes, or the lease cap would drop to 4. The verifier mac also covers `erase_salt`, so once the blob is erased no copy of a record tests a guess; the registration record would need the same binding, for example a wrap under a key from `erase_salt`.
- The OPRF key would come from the pepper and the message id, with the record's `pepper_id`, so that a pepper rotation keeps live messages open through the previous pepper, as the verifier mac does now.
- A claim or burn would send KE1 (96 bytes) with its challenge request and get KE2 (320 bytes) back, then carry KE3 (64 bytes) in place of `X-Nylzo-Verifier`. KE2 is the guessing oracle, so the server would count it as a failed attempt and take the count back only for a valid KE3, as RFC 9807 §9.2 advises. That puts a database write on a passphrase challenge, which F03 kept free of writes.
- Both cores would implement OPAQUE-3DH and check it against the RFC 9807 test vectors. The web client has ristretto255 and RFC 9497 OPRF in `@noble/curves` 2.4.0, which it already ships; Web Crypto has neither. The Go module already depends on `filippo.io/edwards25519`, which has no ristretto255 encoding. `bytemare/opaque`, written by an author of the RFC, states that it has not been independently audited.
- New protocol vectors, a `client_outdated` answer for older clients, e2e coverage, and a measurement of the JavaScript it adds to the web client.

Whether to start protocol v3 with OPAQUE is an open decision for the project owner, not an accepted risk. Until it is made, the logging rules, the refusals above and the generated passphrase are the protection.

## Sources

- [RFC 9807: The OPAQUE Augmented PAKE Protocol](https://www.rfc-editor.org/rfc/rfc9807.html), July 2025, Informational (IRTF CFRG)
- [RFC 9497: Oblivious Pseudorandom Functions Using Prime-Order Groups](https://www.rfc-editor.org/rfc/rfc9497.html)
- [draft-irtf-cfrg-cpace](https://datatracker.ietf.org/doc/draft-irtf-cfrg-cpace/), revision 21 of April 2026
- [OWASP Logging Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html#data-to-exclude): access tokens, authentication passwords and primary secrets stay out of logs
- [Caddy `log` directive](https://caddyserver.com/docs/caddyfile/directives/log): which request headers Caddy redacts by default
- [bytemare/opaque](https://github.com/bytemare/opaque)
