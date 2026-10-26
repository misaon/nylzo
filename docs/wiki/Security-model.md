# Security model

Nylzo encrypts message content on the sender's device. Its URL fragment carries the decryption secret; browsers do not send that fragment in HTTP requests. The [protocol](../../spec/protocol.md) defines the cryptography and request rules. The [security policy](../../SECURITY.md) explains disclosure and supported versions.

Nylzo is early software and has not had an independent security audit. Automated checks, a verified Git signature, and build provenance do not certify the product's security.

## The client is part of the trust boundary

Someone who controls an instance or its hosting can serve modified JavaScript that reads secrets in the browser. Use a CLI from a [verified release](Verify-a-release.md) when you need to avoid trusting the served web client.

`nylzo verify-client` compares an instance's client bytes, build identity, and security headers with the client embedded in the binary. It establishes what the server sent to that check. A hostile server can recognize the CLI and send different code to a browser. The [CLI guide](../cli.md#verify-an-instances-web-client) describes this limit.

## A link grants capabilities

Anyone holding the link can delete a message or exhaust its passphrase attempts, even when a passphrase protects the content. Someone with both the link and a copy of the server's data can guess a weak passphrase offline. Choose a strong generated passphrase and share it separately when the extra protection matters.

Expiry and open limits control access to the server's copy. They cannot erase a recipient's screenshot, saved file, clipboard, backup, or memory. Encryption also cannot protect a compromised sender or recipient device.

## Metadata still exists

The server observes padded content sizes and request timing. Some retained times are rounded to minutes. Logs and a low-traffic instance can still reveal timing correlations; encryption is not anonymity.

Inspect an instance's `/transparency` page for its operator, limits, storage and logging policy, web-client hashes, and client/server build identities. These declarations help inspection; a hostile operator can lie about its running service.

## Verify the software you run

[Verify a release](Verify-a-release.md) against the canonical repository, exact tag, and release workflow. Version strings and server JSON identify a build; they are not cryptographic proof. A signed checksum manifest binds the downloaded files to the publishing workflow. Provenance identifies their source and build workflow. An independent rebuild checks whether the same source produces the published bytes.

Report vulnerabilities through [private reporting](https://github.com/misaon/nylzo/security/advisories/new). Report abusive content through the affected instance's `/report` page.
