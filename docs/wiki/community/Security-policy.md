# Security policy

This page is for anyone who finds a security flaw in Nylzo: how to report it privately and what happens next.

## Report a vulnerability

Report it through GitHub: [Report a vulnerability](https://github.com/misaon/nylzo/security/advisories/new). Do not open a public issue, pull request or discussion about it.

Include:

- the component (protocol, Go or TypeScript core, server, CLI, web client, Compose example or release pipeline) and the version or source commit;
- steps or a proof of concept that reproduces it;
- what an attacker gains and from which position, for example a network observer, someone who holds the link, or someone with a copy of the server's data.

Never send real message links, passphrases or other people's data; a link made on a local test server is enough.

Test only systems you own or may test, preferably a local instance. Avoid denial of service, social engineering, persistence on others' infrastructure and other people's messages.

To report a message on an instance that carries fraud, malware or illegal content, write to the operator contact on that instance's `/transparency` page, if it lists one. This policy covers flaws in Nylzo itself.

## Known limits

The [security model](../guides/Security-model.md) and [Protocol §13](../reference/Protocol.md#13-what-a-stolen-server-reveals) describe limits that are part of the design, such as a hostile server sending a modified web client, or offline guessing of a weak passphrase by someone who has both the link and the server's data. A documented limit alone is not a vulnerability; a report that shows one is worse than documented is welcome.

## What happens next

1. The maintainer aims to acknowledge your report within five business days. This is a best-effort target: Nylzo has one maintainer and no paid bug bounty. If you hear nothing, add a comment to the private report.
2. Impact, a fix and a disclosure date are agreed in the private report. Please wait for the fix before you disclose.
3. The fix ships in the next release. A confirmed vulnerability gets a GitHub Security Advisory, a CVE where appropriate, and credit as you prefer.

## Supported versions

Only the latest stable release gets security fixes. Older versions and prerelease builds are not maintained.

## Official sources

Install the CLI with Homebrew or Scoop from [misaon/homebrew-nylzo](https://github.com/misaon/homebrew-nylzo), download archives from the [Releases page](https://github.com/misaon/nylzo/releases), and pull images from `ghcr.io/misaon/nylzo` and `ghcr.io/misaon/nylzo-caddy`. [Verify a release](../guides/Verify-a-release.md) shows how to check them.
