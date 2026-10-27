# Security policy

## Reporting a vulnerability

Report it privately through GitHub: [Report a vulnerability](https://github.com/misaon/nylzo/security/advisories/new). Please do not open a public issue, pull request or discussion for it.

Include:

- the component (protocol, Go core, TypeScript core, server, CLI, web client, Compose example or release pipeline) and the version or commit;
- the steps or a proof of concept that reproduces it;
- what an attacker gains and from which position, for example a network observer, someone holding the link, or a copy of the server's data (see the [security model](../guides/Security-model.md)).

Do not send real message links, passphrases or other people's data. A link made on a local test server is enough.

To report a message on a public instance that carries fraud, malware or illegal content, use the instance's `/report` page instead. This policy covers flaws in Nylzo itself.

## Scope

In scope: [the protocol](../reference/Protocol.md), both cores and their shared vectors, the server, the `nylzo` CLI, the web client, `deploy/compose` and the release pipeline (signatures, provenance, reproducible builds).

Some limits are known and documented in [the protocol](../reference/Protocol.md) §13, so a report of them alone is not a vulnerability:

- Whoever controls the running server or its hosting can send a modified web client and read the messages created or opened in it. The CLI from a signed release protects against this. `nylzo verify-client` detects modified files served to that check. A hostile server can recognize the command and send browsers different JavaScript.
- Anyone who has the link can delete the message, also when a passphrase protects it, and can lock readers out with wrong passphrases.
- Someone with both the link and a copy of the server can guess a weak passphrase offline.
- The server sees the padded size of a message and times to the minute.

A report that shows one of these is worse than documented is welcome.

## Supported versions

Only the latest stable release receives security fixes and ordinary bug fixes. Support begins when that release is published and ends when the next stable release is published, including a patch release. There are no LTS branches or fixed-duration support commitments. Older versions and pre-release builds are not maintained. If maintenance ends without a successor release, announce that end of support in the repository and release notes. Upgrade to the latest stable release before relying on a fix. For reports about a development build, include its source commit.

## Coordinated disclosure

The maintainer aims to acknowledge reports within five business days. This is a best-effort target, not a guaranteed response time or a paid bug bounty. GitHub notifications are the primary contact channel; if there is no acknowledgement, follow up in the private report or contact [email@ondrejmisak.cz](mailto:email@ondrejmisak.cz) without including exploit details in the email.

Please allow investigation and a coordinated fix before public disclosure. The maintainer will discuss impact, mitigation, and a disclosure date in the private report. Confirmed vulnerabilities should receive a GitHub Security Advisory and a CVE where appropriate, with credit according to the reporter's preference. There is no external security certification implied by this policy or by passing CI.

Test only systems and data you own or have explicit permission to test. Prefer a local instance. Avoid denial of service, social engineering, access to other people's messages, and persistence on third-party infrastructure.

## Official sources and artifact verification

The canonical source is [misaon/nylzo](https://github.com/misaon/nylzo). Download releases only from its [Releases page](https://github.com/misaon/nylzo/releases) and container images from `ghcr.io/misaon/nylzo`. A checksum alone does not establish authenticity: verify its Sigstore signature and the artifact's provenance against this repository and the exact release tag. Use [Verify a release](../guides/Verify-a-release.md) and the [release checklist](../maintainers/Release-checklist.md).

## Fixes and releases

Fixes go into the next release. There are no maintained older branches, so update to the latest release. See [Verify a release](../guides/Verify-a-release.md) for image signatures and provenance, and the [CLI guide](../guides/CLI.md#verify-an-instances-web-client) for checking the client a server sends with `nylzo verify-client`.
