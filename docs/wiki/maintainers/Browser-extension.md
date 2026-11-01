# Browser extension

Status: design, the next step after the local client. Nothing here is built yet.

The extension, working name "Nylzo Verified", is a verified client for people who open links in a browser rather than a terminal. It carries the same web client as the `nylzo` binary and opens an instance link inside the extension before the browser sends any request to the instance. [Verified clients](../guides/Verified-clients.md) explains the guarantee for users; the [local client rules](../reference/Protocol.md#14-client-code-integrity) are the model for its behaviour.

## Design

- Targets: Chrome and Edge, Firefox on desktop and Android, Safari on macOS and iOS. Chrome on Android has no extensions, so its users need a native app.
- Content: the `dist` of the release, with the instance address and the link origin taken from the extension's settings instead of `location.origin`.
- Interception: one `declarativeNetRequest` redirect rule per configured instance, for example `^https://nylzo\.com/r(#.*)?$` to `read.html?i=nylzo.com\1`. In Chromium 156 the rule sees the URL with its fragment, so the key reaches the extension page. Measured by the October 2026 audit: a click on a link reached the extension page with the fragment intact and no page or asset request reached the instance.
- Permissions: host permission only for the configured instances.
- Release: the release workflow builds the extension package reproducibly and lists it in the signed `checksums.txt`, so anyone can compare the store copy with the release.
- A link carries the message id in the fragment, so the rule matches only the `/r` path.

## Server change

The cross-origin guard refuses an extension today: its requests carry `Origin: chrome-extension://…` (or `moz-extension`, `safari-web-extension`) and `Sec-Fetch-Site: none`, and `POST …/burn` answered 403 `cross_origin` in the audit. The extension's pull request adds one rule to `internal/server/crossorigin`: a mutation whose `Origin` has one of these schemes, a host and nothing else, and whose `Sec-Fetch-Site` is `none`, counts as a native client, because no web page can send that origin. The request marker and the signatures stay required. Until then the guard keeps refusing such requests, and `TestBrowserMutationProtection` pins that.

## Open questions before release

- Firefox and Safari: whether their `declarativeNetRequest` keeps the fragment in a redirect is untested.
- Store accounts for Chrome, Firefox and Safari are owner actions; the Chrome Web Store charges a one-time registration fee and Safari needs an Apple developer membership. The publisher account and the store become trust points: a malicious update would reach every user of that version.
- Tests: the hostile-server end-to-end test of the local client (`web/e2e/tests/security/verified-client.spec.ts`) becomes the model; the extension must pass the same check in each engine.
