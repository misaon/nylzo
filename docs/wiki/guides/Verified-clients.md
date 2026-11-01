# Verified clients

Nylzo encrypts and decrypts in the client, so the server stores only ciphertext. What the server can learn depends on where the client's code comes from.

## Which client gives which guarantee

| Client | Where its code comes from | What a server under someone else's control can learn |
| --- | --- | --- |
| The website, in any browser | The server, on every visit | Everything typed or opened on that page: the link with its key, the passphrase and the text. You cannot notice it, and neither can anyone else, because the server can send other code to everyone who checks. |
| `nylzo web` and `nylzo open` (the local client) | The signed `nylzo` binary on your computer | Only encrypted API calls, the same as from the command line. |
| The `nylzo` command line | The signed `nylzo` binary | Only encrypted API calls. |
| The browser extension (planned) | A signed, reproducible package installed in your browser | Only encrypted API calls, for links you open in that browser. |

"Under someone else's control" covers a stolen or hacked server, its hosting provider, a CDN in front of it, and anyone who holds a valid certificate for its name on your network path.

A message stays safe from such a server only when both the sender and the reader use a verified client. The sender's page sees the plaintext and the key before anything is encrypted, and the reader's page sees them after decryption.

## Why the website cannot be verified

Your browser does not send the part of the link after `#` to the server. The page that reads that part, though, also comes from the server. In October 2026 no browser checks the code of an ordinary website against a published or logged version before running it:

- WAICT, the planned web standard for this, has only an early prototype behind a preference in Firefox Nightly ([Mozilla, 5 May 2026](https://hacks.mozilla.org/2026/05/trustworthy-javascript-for-the-open-web/)).
- WEBCAT works only in Firefox with its alpha extension ([WEBCAT FAQ](https://docs.webcat.tech/faq.html)).
- Isolated Web Apps install only on managed ChromeOS devices ([Chrome for Developers](https://developer.chrome.com/docs/iwa/introduction)).

`nylzo verify-client`, the hourly monitor and the signed release manifest find a changed client that the server sends to everyone. They cannot see what one browser received. The website serves the same bytes and the same Content-Security-Policy on every response, so it can join WEBCAT or WAICT once a browser enforces them.

## Use the local client

Install a [verified release](Verify-a-release.md) of `nylzo`.

To write a message:

```sh
nylzo web --server https://nylzo.example
```

Your browser opens the familiar page at a random address such as `http://127.0.0.1:53211/`. The link it creates points to `https://nylzo.example`, so the reader can open it with any client. Press Ctrl-C in the terminal when you are done. The address changes every time, so the browser does not keep the list of remembered messages between sessions.

To read a message, copy the link and run:

```sh
nylzo open --server https://nylzo.example
```

Paste the link at the hidden prompt and press Enter. You can also pipe it in (`pbpaste | nylzo open --server …`) or use `--link-file`, and set `NYLZO_SERVER` once instead of `--server`. The browser opens the message from the local client. The link never appears in a command, in the address bar or in the browser history: the browser receives a one-time address that works once, for two minutes, and the page then takes the link from the local client in memory. If that address is used twice, the command warns you, because another program may have taken the link. Press Ctrl-C when you are done reading.

`nylzo open` refuses a link of another instance than the one you name, so a link that points somewhere unexpected does not open.

## What the local client does

- It serves the web client embedded in the binary, with the same security headers and Content-Security-Policy as the instance.
- It listens only on `127.0.0.1`, on a random port, and answers only requests for that exact address, so a web page that rebinds its DNS name to your computer reaches nothing.
- It sends only API calls to the instance, the way the command line does: without cookies, the page address or browser details. It accepts them only from its own pages.
- Nothing the instance answers can run as code in the local page.

The [protocol, section 14](../reference/Protocol.md#14-client-code-integrity), states these rules exactly.

## What no client protects against

- Malware or another user with administrator rights on your computer or the reader's.
- Another browser extension that may read the pages of the local client or of the website.
- A recipient who shares the message.
- A compromised release. Releases are signed, publicly logged and reproducible, so such a change would reach every user and could be found; see [Verify a release](Verify-a-release.md).

## The browser extension

The next verified client is a browser extension that carries the same web client and opens instance links before any request leaves the browser. It is not available yet; [Browser extension](../maintainers/Browser-extension.md) describes the design.
