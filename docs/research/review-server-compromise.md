# Server-compromise review of the Nylzo design (2026-10-21)

Reviewer position: adversary with the design document and appendices A to H only. Claim under test: "If someone steals, breaks into, or injects code into the server, the attacker learns nothing, discovers no data, and no secrets are revealed."

## a. Verdict

The claim holds for message content against every passive position (stolen database, storage, configuration, backups, process memory, traffic), because S never reaches the server and R, V and K are independent HKDF outputs. It holds only partially for metadata and operational secrets, which every position leaks: ids, near-exact file sizes, creation and opening times, failure counts, and with the pepper the IPv4 address behind every counter row. It fails for "injects code": the web client is embedded in the same binary, so whoever changes the server changes the client and reads every message created or opened in a browser from that moment, and the design's own threat model understates that the Scaleway TLS edge is in the same position.

Wording the owner can publish:

> Content is encrypted in your browser or CLI with a key that never leaves it. A stolen copy of our database, object storage and server configuration does not let anyone read a message. It does reveal message identifiers, approximate sizes, creation and opening times, open counts and, together with the server's configuration secret, which IP addresses used the service in the preceding days. If the server, the hosting provider's edge, or the web client code we serve is altered, by an attacker or by us, every message created or opened in the web client after that moment can be read. Messages handled only with a verified CLI release stay protected. Your browser cannot check the client code it receives; we publish source, reproducible build hashes and signed releases so that others can check what we ship.

## b. What each position obtains

| Position | Content | Metadata | Tokens and capabilities | Future messages |
|---|---|---|---|---|
| (a) DB dump only | None; no ciphertext in the DB | All live ids, state, kdf_id, exact ciphertext size, created_at, expires_at, first_opened_at, ended_at, opens, fail counts; how many IP hashes attacked which id; per-IP-hash counters (not reversible) | mac(h(R)), mac(h(V)), mac(M), mac(L): cannot verify, recover or forge without the pepper | None; the ids are enough to mass-report via /abuse |
| (b) Storage only | Ciphertext; useless without S | File size to within the meta JSON length (padding covers text-only messages); object LastModified equals creation time | None | None; write access gives DoS only (GCM tag fails) |
| (c) DB + storage | None | Union of (a) and (b) | As (a) | None; a later link leak (S) allows offline passphrase guessing against the GCM tag, pepper or not |
| (d) + config (pepper, S3 keys, DB creds) | None | As (c), plus every IP hash becomes an IPv4 address (2^32 HMACs per day salt, about an hour on one core) or a suspect IP is tested directly; covers hot rows and the 7-day DB backups | Can verify candidate R/V/M/L, cannot recover them (256-bit random inputs); live DB write lets it substitute its own macs and claim through the API, which yields ciphertext it already holds and leaves an "opened" trace; S3 keys allow deletion and tampering (integrity failure) | None without code execution; the pepper stays valid until rotated and v1 has no rotation |
| (e) TLS edge, process memory, traffic | None; S never in flight; R, V, K are independent PRF outputs | Raw client IPs of creator and every reader, with id and time (T11 complete) | Create response: M. Metadata, claim, burn: R and V. Claim response: L and ciphertext. Can read metadata and status, front-run or replay a claim, burn opens, download ciphertext, burn, cancel | Passive: none. Active edge rewrites the served HTML and JS, so it equals (g). Memory adds pepper and credentials, so it equals (d) |
| (f) Code injection into the Go server | As (e); serves a modified client | Everything | Everything | All messages created or opened in the web client after injection, including ones created earlier; CLI users keep content, lose metadata and availability |
| (g) Modified web client | S from the fragment, P from the input, K, plaintext | Everything the browser sees | M in localStorage, L in sessionStorage | As (f) for web users; CLI users unaffected |
| (h) Build pipeline, GitHub Actions | As (f) | As (f) | As (f) | CLI and MCP users too; attestations stay valid because they bind to whatever source and workflow ran |
| (i) Scaleway insider, legal order | None at rest | (d) plus (e): raw IPs at the ingress, secret env vars, DB, storage, DB backups, undocumented ingress logs | All in-flight tokens; can run a modified image | Yes, by altering the image or response bodies; a legal order yields metadata and ciphertext, not content, unless the served client is altered |
| (j) Provider backups, replicas | Ciphertext remnants without S | DB backed up daily and kept 7 days (verified): message rows, counters, attempts and tombstones outlive the design's purge times by up to 7 days; object storage has no backups by default, versioning off, lifecycle 8 days | Macs only; with (d) the backed-up IP hashes are reversible | With S, a backed-up verifier_mac and the pepper: offline passphrase guessing after deletion |

## c. Findings

1. High. The TLS edge is in the client-code path and the threat model treats it as a replay risk. Scaleway "generates a dedicated TLS certificate for that custom domain", terminates TLS and talks h2c to the container. Whoever controls that ingress rewrites index.html and the JS on the way out, which is position (g). T13 and OD18 only mention burning opens. Fix: state that on the public instance Scaleway is in the trusted base for client integrity, same as the operator; keep OD18 with that reason instead of "the edge can delete anyway".

2. High. Published hashes and reproducible builds verify the release, not what a visitor received. A targeted injector serves the clean bundle to the verifier and the modified one to the victim; `verify-client` fetches from the same server and inherits the problem. Browser-side detection does not exist in October 2026: WAICT is "a prototype implementation in Firefox Nightly" with a draft specification, Isolated Web Apps are enterprise ChromeOS, Code Verify covers Meta properties, and SRI pins only `<script>` and `<link>` loaded by an HTML document the attacker also serves. Fix for v1: say the browser cannot verify; make a verified CLI release the documented answer for anyone who distrusts the operator; run a monitor on another network that fetches the bundle hourly and compares it with the release hash (catches blanket injection from a compromised image, the realistic case); publish a WAICT-style manifest at a well-known path now so opt-in costs nothing later. A separate static origin (Codeberg or GitHub Pages serving HTML and JS with SRI, API on Scaleway) forces an attacker to compromise two parties and belongs in v2. It costs CORS and CSP work, breaks "one binary, one hash" (KD17), and sends every visitor's IP to a second party, non-EU in GitHub's case, which the design's EU table tolerates for JS and CSS.

3. Medium. The pepper has no key id and no rotation. After a suspected leak the operator chooses between killing every live message and keeping a known-bad pepper, and most will wait. A leaked pepper also de-anonymises the IP counters retroactively through the backups. Fix: one-byte `pepper_id` per row, `NYLZO_PEPPER` plus `NYLZO_PEPPER_PREVIOUS`, verify with the row's pepper, write with the current one. Rows expire within 7 days, so nothing needs rewriting.

4. Medium. Hashed IP counters are reversible for anyone with the pepper, and `passphrase_attempts` joins id to IP hash, the most sensitive link in the schema. Fix: hash with a separate `pepper_ip`, truncate to 24 bits so each bucket covers about 256 IPv4 addresses, hash IPv6 at /64, purge hourly rows an hour after the window, delete `passphrase_attempts` rows when the message ends. Rate limiting loses nothing measurable.

5. Medium. Provider backups contradict the retention statements. Serverless SQL databases "are automatically backed up every day at the same time" and "Backups are stored for 7 days", while the design says counters purge within 24 h and OD20 says no backups. Fix: the transparency page states that database metadata can persist up to 7 days in provider backups; the R13 test checks whether backups can be disabled; the DB holds as little as findings 4 and 8 allow.

6. Low. M is generated by the server and returned in the create response, so the edge and process memory hold a cancel capability for every message from the first second. Fix: the client generates M, sends h(M) at create and M on cancel and status, mirroring R. The server then never produces a sender secret.

7. Low. File sizes leak nearly exactly (KD14). Fix: pad files to a multiple of 64 KiB and text to 4 KiB; at most 64 KiB extra per message, about 1% on a 5 MiB file.

8. Low. A DB-only attacker learns every live id and the abuse process acts by id. Mass reports become a cheap takedown. Fix: rate limit and PoW on /abuse, human review before deletion, abuse log keyed by hashed id.

9. Medium. Position (h) defeats the only mitigation that works against (f) and (g), and the plan has no source-side controls. An attestation links "to the source code and the build instructions" and is "not a guarantee that an artifact is secure"; SLSA L3 prevents tampering during the build, not in the source. Fix: branch and tag protection with required review, signed tags, a release checklist in which the maintainer rebuilds locally (`CGO_ENABLED=0 go build -trimpath` is reproducible per the Go team) and publishes that hash next to the CI hash, and a documented `gh attestation verify` plus cosign step for CLI users.

10. Info. mac(h(R)) versus mac(R). At rest the inner hash adds nothing: once the pepper is known, HMAC is a public function and a 256-bit random preimage is out of reach either way. The hash matters on the wire: the create request carries h(R), which cannot be replayed as a claim, so an edge that watches creation cannot open the message. The spec should say this; it reads as an unexplained choice.

11. Info. Challenge-response for OD18 has no cheap form. The server holds mac(h(R)) only, so it cannot check HMAC(R, nonce); a nonce-bound proof needs a keypair derived from S (a signature over a server nonce), which adds asymmetric crypto to the application layer and the audit. Against an edge that can also rewrite the client it buys little. Keep the v1 decision.

12. Info. "Logs contain no IP permanently" describes the application log. Scaleway's ingress sees every client IP in cleartext and the design's own table says its log location is unspecified. The transparency page should say so.

## d. Metadata minimization options

| Change | What a compromise stops revealing | Cost |
|---|---|---|
| Pad all payloads (finding 7) | Exact file size | At most 64 KiB per message; trivial code |
| Truncated IP hashes with separate pepper, IPv6 /64, hourly purge, delete attempts at message end (finding 4) | Which IP used the service, which IP attacked which message | None measurable |
| Per-id-only passphrase backoff, no IP in the passphrase path | The id-to-IP join entirely | An attacker imposes the backoff on the real recipient after 6 failures instead of 100; the sender sees it in status and resends. Config switch, default off |
| `first_opened_at` at minute precision | Sub-minute correlation with network logs | None visible; OD8 still shows "opened at T" |
| Tombstones 48 h instead of 7 days | "Opened at T" after 48 h | Late link holders get "unavailable"; provider backups keep the row 7 days anyway, so only the hot DB gains |
| Log route patterns, not ids | Id-to-time correlation in the application log | Harder forensics; the abuse report carries the id anyway |
| Blur `created_at` | Nothing: `expires_at - ttl_s` and the object's LastModified give it back | Skip it |
| Drop fail counters | Nothing worth having; the sender-facing count is a feature | Skip it |

## e. What not to change

AES-256-GCM with one key and one random 96-bit IV per message. HKDF domain separation of R, K and V from S, pending the planned external review. h(R) and h(V) at create, R and V at claim. HMAC with a pepper outside the database (add the key id). Online passphrase limiting that never burns an open. Policy in AAD. The embedded client for v1, with the consequence written down. OD18 as decided. No application-layer asymmetric crypto in v1. No certificate pinning in the CLI: the leaf is a Scaleway-issued Let's Encrypt certificate that rotates, and the CLI's protection comes from release verification. No "zero knowledge" or "nothing leaks" on any page.

## f. Sources opened

- https://www.scaleway.com/en/docs/serverless-containers/how-to/add-a-custom-domain-to-a-container/ "Scaleway generates a dedicated TLS certificate for that custom domain so it can respond to HTTPS requests." VERIFIED (body at offset 100000).
- https://www.scaleway.com/en/docs/serverless-containers/faq/ "Scaleway uses Let's Encrypt to generate and automatically renew certificates on your Custom Domains."; "Load Balancing is integrated directly to Serverless Containers when you invoke them." VERIFIED.
- https://www.scaleway.com/en/docs/serverless-containers/concepts/ "The container supports http1 (default) and http2 (h2c)"; "Scaleway Serverless Containers automatically inject several HTTP headers into every incoming request."; secrets "are environment variables that are injected into your container with extra security." VERIFIED. Request-headers reference page: 404 at two guessed paths, NOT FOUND.
- https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-limitations/ "Serverless Containers support HTTP/1.1 and HTTP/2 connections."; secret env var max "65536 bytes". VERIFIED. Body size limit: NOT FOUND.
- https://www.scaleway.com/en/docs/serverless-sql-databases/how-to/manage-backups/ "Serverless SQL Databases are automatically backed up every day at the same time."; "Backups are stored for 7 days." VERIFIED. Whether backups can be disabled: NOT FOUND.
- https://www.scaleway.com/en/docs/object-storage/concepts/ versioning "allows you to keep multiple variants of an object in the same bucket", opt-in per bucket. VERIFIED. Replication and durability: NOT FOUND on the page.
- https://pkg.go.dev/embed "Package embed provides access to files embedded in the running Go program."; "An FS is a read-only collection of files". VERIFIED.
- https://go.dev/blog/rebuild "Go 1.21.0 is the first Go toolchain with perfectly reproducible builds."; "a reproducible build is as simple as compiling with CGO_ENABLED=0 go build -trimpath". VERIFIED.
- https://docs.github.com/en/actions/concepts/security/artifact-attestations "Artifact attestations by itself provides SLSA v1.0 Build Level 2."; "artifact attestations are not a guarantee that an artifact is secure." VERIFIED.
- https://slsa.dev/spec/v1.0/levels Build L3: "Prevents tampering during the build—by insider threats, compromised credentials, or other tenants." VERIFIED.
- https://docs.sigstore.dev/cosign/signing/overview/ "Fulcio issues short-lived certificates binding an ephemeral key to an OpenID Connect identity."; Rekor provides "an auditable record of when a signature was created." VERIFIED.
- https://developer.mozilla.org/en-US/docs/Web/Security/Subresource_Integrity SRI applies to `<script>` and `<link>` with `rel` stylesheet, preload or modulepreload; nothing covers the document itself. VERIFIED.
- https://attackanddefense.dev/2026/07/12/firefox-security-privacy-newsletter-2026-q2.html WAICT: "a prototype implementation in Firefox Nightly that works with our WAICT Demo and a draft specification." VERIFIED. hacks.mozilla.org post returned 403, NOT OPENED.
- https://github.com/facebookincubator/meta-code-verify extension "confirms that your Facebook, Messenger, Instagram, and WhatsApp Web code hasn't been tampered with"; third-party use not documented. VERIFIED.
- Isolated Web Apps: web search only; Chrome 128 launch limited to enterprise-managed ChromeOS, no 2026 desktop release found. PARTIAL.
