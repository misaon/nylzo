# Security model

Nylzo encrypts message content on the sender's device. Its URL fragment carries the decryption secret; browsers do not send that fragment in HTTP requests. The [protocol](../reference/Protocol.md) defines the cryptography and request rules. The [security policy](../community/Security-policy.md) explains disclosure and supported versions.

Nylzo is early software and has not had an independent security audit. Automated checks, a verified Git signature, and build provenance do not certify the product's security.

## The client is part of the trust boundary

Someone who controls an instance or its hosting can serve modified JavaScript that reads secrets in the browser. Use a CLI from a [verified release](Verify-a-release.md) when you need to avoid trusting the served web client.

`nylzo verify-client` compares an instance's client bytes, build identity, and security headers with the client embedded in the binary. It establishes what the server sent to that check. A hostile server can recognize the CLI and send different code to a browser. The [CLI guide](CLI.md#verify-an-instances-web-client) describes this limit.

## Browser resource integrity

The production client bundles its scripts, styles and fonts locally. The final build parses the emitted JavaScript module graph and CSS URLs, rejecting external imports, computed module imports, missing assets and unbundled CSS imports. It adds SHA-384 SRI to script and stylesheet elements, including stylesheets created during prerendering. A generated import map supplies SHA-384 metadata for static and dynamic document-module imports in supporting browsers. Browser tests reject modified entry scripts, dynamically imported modules and stylesheets. The `Integrity-Policy: blocked-destinations=(script)` header also rejects script fetches without integrity metadata in supporting browsers. Chromium tests enforce this missing-metadata case. Stylesheets always receive SRI attributes and hash-mismatch tests; the header does not advertise the style destination, which the tested Chromium does not implement. Unsupported browsers retain element SRI and CSP protections.

The HTTP CSP requires a fresh nonce. An additional CSP at the start of the document restricts scripts and stylesheets to the same origin, even when an element has a valid nonce. Its script hash permits only the generated import map. The additional policy's `style-src 'unsafe-inline'` does not remove the HTTP policy's nonce requirement: browsers enforce both policies together. Trusted Types, same-origin worker restrictions and the existing connection policy remain enforced.

Import-map integrity applies to the document module loader, not the initial Worker fetch. The KDF worker is bundled locally, restricted to its exact build URL by Trusted Types where available, and covered by the signed release and `verify-client` comparison. No remote loader or compatibility polyfill is fetched. Browsers without import-map integrity support still enforce element SRI and CSP; they do not gain integrity checking for imported modules from the map.

SRI protects a resource relative to its expected hash. An attacker who can replace the HTML can replace its hashes and policies too. These controls do not remove the trust boundary described above.

References: [Subresource Integrity](https://developer.mozilla.org/en-US/docs/Web/Security/Defenses/Subresource_Integrity), [import-map integrity](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/script/type/importmap#integrity_metadata_map), [multiple CSP policies](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy#multiple_content_security_policies).

## Browser keys and memory

AES-256-GCM encryption and decryption, HKDF, hashing and Ed25519 signing use the browser's `crypto.subtle` API. Content keys are derived with `extractable: false` and kept in memory. Read and verifier signing keys are imported as non-extractable keys; no private key is exported to obtain its public half. A PKCS#8 import failure can retry a non-extractable JWK import. If native signing is unavailable, reading fails rather than retaining a raw signing seed.

Two operations still use the bundled Noble libraries: the protocol's scrypt password derivation, which Web Crypto does not provide, and public-key derivation from a seed, which has no interoperable browser API. Scrypt runs in a dedicated worker that terminates after each operation. This does not change the protocol, password cost or encrypted format.

When a sender chooses to remember a message, IndexedDB stores its non-extractable Ed25519 management key through structured cloning. That key can check status or cancel a message; it cannot decrypt its content. Both reads and writes validate the key type, algorithm, permitted usage and non-extractability.

An interrupted read can resume without spending another open. The tab's session storage holds a random pointer and a server lease, never a plaintext fragment. IndexedDB holds the fragment encrypted with AES-256-GCM and a fresh, non-extractable 256-bit key. Authentication binds the ciphertext to the message, pointer and expiry. Recovery expires after the maximum supported transfer time plus 60 seconds; expired records are rejected and purged on subsequent store access. Acknowledgement or a terminal message state removes the pointer and requests deletion of the record. If storage fails, reading can continue without recovery; the client does not fall back to plaintext storage. Old plaintext recovery entries are rejected, so an interrupted read from an older client may require reopening the original link.

Mutable secret buffers are overwritten after use, including error paths, and references to content keys and fragments are released when no longer needed. JavaScript strings, browser-managed keys, garbage collection, disk remnants and device backups cannot be reliably zeroized by a page. Non-extractability blocks `exportKey` and `wrapKey`; hostile same-origin JavaScript can still ask the browser to use a key. It does not promise hardware-backed storage, encryption of the browser profile or protection from a compromised device.

References: [CryptoKey extraction rules](https://developer.mozilla.org/en-US/docs/Web/API/CryptoKey/extractable), [Web Crypto algorithms](https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto#supported_algorithms), [structured cloning](https://developer.mozilla.org/en-US/docs/Web/API/Web_Workers_API/Structured_clone_algorithm), [Web Crypto security considerations](https://www.w3.org/TR/webcrypto-2/#security-developers) (working draft).

## A link grants capabilities

Anyone holding the link can delete a message or exhaust its passphrase attempts, even when a passphrase protects the content. Someone with both the link and a copy of the server's data can guess a weak passphrase offline. If you add a passphrase, use a generated one and send it through a different channel.

Expiry and open limits control access to the server's copy. They cannot erase a recipient's screenshot, saved file, clipboard, backup, or memory. Encryption also cannot protect a compromised sender or recipient device.

## Metadata still exists

The server observes padded content sizes and request timing. Some retained times are rounded to minutes. Logs and a low-traffic instance can still reveal timing correlations; encryption is not anonymity.

Large attachments may be gzip-compressed on the sender's device before encryption when the instance advertises support. New clients keep the original format when connecting to an older instance. Text and other messages are excluded from each file's compression context. The visible padded size then reflects compressed content; padding does not remove all length-based inference. Do not combine attacker-controlled bytes with secrets in a file if the attacker can repeatedly observe its compressed size. Readers authenticate before decompressing and enforce the original file-size limit. Older CLI versions must be updated to read compressed attachments. See the [protocol](../reference/Protocol.md#32-compressed-attachments).

Inspect an instance's `/transparency` page for its operator, limits, storage and logging policy, web-client hashes, and client/server build identities. These are the operator's declarations. A hostile operator can falsify them.

## Verify the software you run

[Verify a release](Verify-a-release.md) against the canonical repository, exact tag, and release workflow. Version strings and server JSON identify a build; they are not cryptographic proof. A signed checksum manifest binds the downloaded files to the publishing workflow. Provenance identifies their source and build workflow. An independent rebuild checks whether the same source produces the published bytes.

Report vulnerabilities through [private reporting](https://github.com/misaon/nylzo/security/advisories/new). Report abusive content through the affected instance's `/report` page.
