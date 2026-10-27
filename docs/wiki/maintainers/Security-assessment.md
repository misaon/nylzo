# Security assessment and threat model

Assessment date: **2026-10-27**. Scope: the published Nylzo 0.1.1 application, its Go server and CLI, browser client, protocol, storage, Compose deployment, and the reviewed release controls. This is a project self-assessment based on source review, protocol requirements, and automated security tests. It is not an independent audit or penetration test.

Review this assessment before a release that changes cryptography, capabilities, message lifecycle, external interfaces, storage, trusted proxies, dependencies, or publishing authority. Update the affected threats, controls, tests, and accepted risks in the same PR. A new protocol primitive or trust boundary needs a fresh assessment rather than an assertion that existing tests still pass.

## Assets and security goals

Protect plaintext, link secrets, passphrases, signing capabilities, server peppers, live-message state, release identities, and administrative credentials. The intended confidentiality property covers stored data and ordinary protocol traffic. It does not cover a compromised sender/recipient device, a hostile browser client served by an operator, or an authorized recipient retaining plaintext.

Availability, expiry, and open counts depend on an honest running server and its persistent state. A server operator can refuse service, delete data, serve modified code, or restore an older database. Expiry cannot revoke copies already obtained by a recipient. These limits are part of the [security model](../guides/Security-model.md), not claims that the implementation can detect every hostile operator.

## Actors, actions, and trust boundaries

| Actor or subsystem | Actions and authority | Boundary |
| --- | --- | --- |
| Sender browser or CLI | Creates keys, encrypts content, uploads an envelope, shares the read link, retains cancellation/status capability | Plaintext and private capabilities remain on the client |
| Recipient browser or CLI | Uses the read capability to claim/download, decrypt, acknowledge, burn, or submit a sealed abuse report | Link possession grants protocol capabilities; a passphrase adds the documented read restriction |
| Anonymous network client | Requests public metadata, creates messages subject to limits, requests challenges, attempts arbitrary API input | All input is untrusted; resource bounds and authorization apply before state changes |
| Server and SQLite/file store | Enforces signatures, leases, expiry, attempt limits, open counts, deletion, and rate limits; stores ciphertext and operational state | Persistent-state transitions must be atomic; server data alone must not contain decryption secrets |
| TLS proxy and hosting operator | Terminates TLS, forwards client addresses, serves the instance and web client, controls host data and availability | Trusted to serve the intended browser code and to supply correct forwarding metadata |
| Link holder or malicious recipient | Can retain plaintext, delete a message, exhaust attempts, or share the capability | Possession is authorization; sender identity and non-repudiation are not provided |
| Attacker with a server backup | Inspects SQLite/WAL, blobs, logs, configuration, and potentially the pepper | Ciphertext confidentiality remains; a stolen link enables the documented offline passphrase attack |
| Abuse-report operator | Holds the reporting private key off-server and decrypts sealed reports | Report decryption authority is separate from application hosting |
| Dependency publisher or compromised registry | Supplies code/tools consumed by builds | Locked versions, integrity verification, restricted build scripts, review, and scanning reduce supply-chain risk |
| Maintainer and release automation | Reviews code or bounded dependency updates, signs tags, publishes artifacts and provenance | Publishing authority is separate from untrusted test execution; signatures identify provenance, not correctness |

## External interfaces

The [protocol API table](../reference/Protocol.md#6-http-api) specifies methods, paths, authorization, and request/response data for create, metadata, claim, blob, ack, burn, cancel, status, instance, challenge, and abuse reporting. Its following sections define envelopes, signatures, leases, errors, transport bounds, and storage rules. [CLI documentation](../guides/CLI.md) defines command-line input, output, exit behavior, and server selection.

The HTTP surface also includes `/healthz`, browser pages, hashed assets, transparency/client manifests, crawler metadata, and the canonical public URL. These are registered in [routes.go](../../../internal/server/routes.go) and documented in protocol §9 and the [operations guide](../guides/Operations.md). Browser URL fragments carry capabilities and are not sent as HTTP request targets. Instance metadata and transparency information are public; operator declarations are not independently verified.

Filesystem interfaces are the configured data directory, SQLite and WAL files, encrypted blobs, sealed abuse reports, pepper files, and the CLI's explicit input/output files. Environment/CLI configuration and trusted-proxy behavior are described in [operations](../guides/Operations.md) and [self-hosting](../guides/Self-hosting.md). The release interface consists of immutable GitHub assets, signed checksum manifests, provenance attestations, SBOMs, and digest-addressed GHCR images.

## Threats, controls, and remaining risk

| Threat | Existing controls and evidence | Remaining risk |
| --- | --- | --- |
| Plaintext or capabilities leak through creation or a stolen store | Client encryption; shared protocol vectors; `TestAK1CreateCarriesNoSecrets` and `TestStolenServerRevealsNothing` inspect traffic and stored artifacts | Compromised devices, a hostile web client, or possession of the link change the attacker's authority |
| Ciphertext/header modification or request substitution | Authenticated encryption and signed request transcripts; `TestAK8TamperingFailsIntegrity`, `TestRequestSignatureBindsEveryField`, `TestSignatureBindsTheRequest` | A link holder can exercise its documented capabilities; no sender identity guarantee |
| Replay after rejection or restart | Replay/state rules; `TestRejectedRequestsCannotBeReplayed` and `TestReplayAfterRestartIsRefused` | Restoring old storage can restore state; do not represent backups as tamper-proof counters |
| Concurrent reads exceed open limits or break lease/deletion state | Transactional lifecycle and lease rules; `TestAK4ConcurrentClaimsHTTP`, `TestFlowCreateMetadataClaimBlobAck`, and lease-expiry tests | A recipient can copy already decrypted data; server availability remains an operator responsibility |
| Guessing passphrases or locking out another reader | Specified KDF, bounded attempt policy, protocol vectors, `TestAK5PassphrasePolicyHTTP` | Weak passphrases remain guessable with a stolen link and server data; a link holder can exhaust attempts |
| Message enumeration, storage faults, or timing differences expose existence | Uniform error policy and isolated timing acceptance tests; `TestAK21UniformNotFoundAndConflict`, `TestStorageFaultKeepsUniformNotFound` | Operational conditions and external network observations need deployment-specific review |
| Creation floods, slow connections, or forged forwarding headers exhaust resources | Body/header/connection limits, source/global caps, proof of work, disk reserve, exact trusted proxies; AK12/AK22 and Compose proxy tests | Distributed attacks and provider outages remain possible; operators must set caps for actual disk capacity |
| Modified browser code, unsafe asset delivery, or redirected CLI traffic exposes secrets | CSP/nonces/security headers, signed release distribution, CLI HTTPS-origin and redirect checks, client-manifest verification tests | A hostile server can recognize a verification request and serve browsers different code |
| CI input injection or stolen publishing credentials substitutes a release | Quoted/validated inputs, SHA-pinned actions, separate permissions, protected tags/environments, keyless signatures, immutable assets, policy-content checks | An owner-account compromise can change repository settings; a trusted writer remains an administrative risk |
| A dependency gains a vulnerability or contains malicious code | Locked Go/npm dependencies, checksum verification, restricted install scripts, dependency review, vulnerability scans, and release artifact scans | Scanners cannot prove the absence of unknown vulnerabilities or malicious behavior |

## Assessment result and maintenance

The experimental [Hetzner/Flatcar profile](../guides/Hetzner-Flatcar.md) introduces local infrastructure state, provider tokens, an image factory, OS updates, and a privileged host updater. Tokens and reporting private keys stay off the VPS; plans/state are encrypted, image downloads are signature-verified, and application updates require signed release identity and provenance. A failed persistent-state migration stops deployment rather than restoring consumed message state. The cloud provider and administrator remain trusted; there is no hardware-attested boot or protection from a hostile host. Real cloud provisioning, TLS, and A/B OS recovery require acceptance before production approval. External failure monitoring and operator response remain necessary.

The highest-impact accepted limitation is browser-client delivery: TLS and client-side encryption do not protect a browser from the operator who serves its code. The signed CLI is the alternative for users who need to choose independently verified client code. The next priority is preserving lifecycle atomicity and confidentiality under faults; the protocol tests, race runs, timing tests, fuzz smoke tests, and cross-language vectors exercise those guarantees.

Supply-chain controls establish artifact identity and gate known findings. They do not establish an independent cryptographic review. The project has one human maintainer and a documented review exception. It therefore does not meet the Baseline Level 3 requirement for non-author human approval on every primary-branch change. A bot approval does not satisfy that requirement.

Track newly discovered vulnerabilities through the [disclosure policy](../community/Security-policy.md), publish affected/fixed versions and mitigations, and update this assessment when findings change a trust boundary. Use synthetic data and a local instance for testing; no assessment here authorizes testing another operator's infrastructure.
