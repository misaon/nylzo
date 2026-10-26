# Security policy

## Reporting a vulnerability

Report it privately through GitHub: [Report a vulnerability](https://github.com/misaon/nylzo/security/advisories/new). Please do not open a public issue, pull request or discussion for it.

Include:

- the component (protocol, Go core, TypeScript core, server, CLI, web client, Compose example or release pipeline) and the version or commit;
- the steps or a proof of concept that reproduces it;
- what an attacker gains and from which position, for example a network observer, someone holding the link, or a copy of the server's data (the threat model is in `docs/designs/nylzo-architecture-research.md`).

Do not send real message links, passphrases or other people's data. A link made on a local test server is enough.

To report a message on a public instance that carries fraud, malware or illegal content, use the instance's `/report` page instead. This policy covers flaws in Nylzo itself.

## Scope

In scope: `spec/protocol.md`, both cores and their shared vectors, the server, the `nylzo` CLI, the web client, `deploy/compose` and the release pipeline (signatures, provenance, reproducible builds).

Some limits are known and documented in `spec/protocol.md` §13, so a report of them alone is not a vulnerability:

- Whoever controls the running server or its hosting can send a modified web client and read the messages created or opened in it. The CLI from a signed release protects against this. `nylzo verify-client` finds a swap that does not avoid it, but a server can recognise the command and send browsers something else.
- Anyone who has the link can delete the message, also when a passphrase protects it, and can lock readers out with wrong passphrases.
- Someone with both the link and a copy of the server can guess a weak passphrase offline.
- The server sees the padded size of a message and times to the minute.

A report that shows one of these is worse than documented is welcome.

## Supported versions

Before the first release, report against the current `main` commit. After releases begin, only the latest stable release receives security fixes; older versions and pre-release builds are not maintained. Upgrade to the latest stable release before relying on a fix.

## Coordinated disclosure

The maintainer aims to acknowledge reports within five business days. This is a best-effort target, not a guaranteed response time or a paid bug bounty. GitHub notifications are the primary contact channel; if there is no acknowledgement, follow up in the private report or contact [email@ondrejmisak.cz](mailto:email@ondrejmisak.cz) without including exploit details in the email.

Please allow investigation and a coordinated fix before public disclosure. The maintainer will discuss impact, mitigation, and a disclosure date in the private report. Confirmed vulnerabilities should receive a GitHub Security Advisory and a CVE where appropriate, with credit according to the reporter's preference. There is no external security certification implied by this policy or by passing CI.

Test only systems and data you own or have explicit permission to test. Prefer a local instance. Avoid denial of service, social engineering, access to other people's messages, and persistence on third-party infrastructure.

## Official sources and artifact verification

The canonical source is [misaon/nylzo](https://github.com/misaon/nylzo). Download releases only from its [Releases page](https://github.com/misaon/nylzo/releases) and container images from `ghcr.io/misaon/nylzo`. A checksum alone does not establish authenticity: verify its Sigstore signature and the artifact's provenance against this repository and the exact release tag. Instructions are in [deploy/compose/README.md](deploy/compose/README.md) and the [release checklist](docs/release-checklist.md).

## Fixes and releases

Fixes go into the next release. There are no maintained older branches, so update to the latest release. See [Verify a release](docs/wiki/Verify-a-release.md) for image signatures and provenance, and the [CLI guide](docs/cli.md#verify-an-instances-web-client) for checking the client a server sends with `nylzo verify-client`.
