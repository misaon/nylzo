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

## Fixes and releases

Fixes go into the next release. There are no maintained older branches, so update to the latest release. How to verify a release image and its provenance is in `deploy/compose/README.md`; how to check the web client a server sends is in the README (`nylzo verify-client`).
