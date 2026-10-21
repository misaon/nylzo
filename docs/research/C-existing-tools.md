# Existing one-time secret, encrypted paste and temporary file sharing tools: documented problems and design lessons

Research date: 2026-10-21. Every claim lists the URL that was opened, a supporting quote of at most 25 words, and a status (VERIFIED, PARTIAL, NOT FOUND). Release dates come from the GitHub REST API where the HTML release page omitted the year. Pages that could not be fetched are listed at the end.

## 1. PrivateBin

Trust model. Client-side encryption; the key is in the URL fragment; the user must trust the operator's JavaScript.
- URL: https://github.com/PrivateBin/PrivateBin
- Quotes: "As a user you have to trust the server administrator not to inject any malicious code." / "The "key" used to encrypt the paste is part of the URL." / "A server admin can be forced to hand over access logs to the authorities."
- Status: VERIFIED.

Crypto details. AES-256-GCM, PBKDF2-HMAC-SHA256 with 100,000 iterations, 8-byte salt, 16-byte IV, 128-bit tag, zlib compression, base58 key in the fragment.
- URL: https://github.com/PrivateBin/PrivateBin/wiki/Encryption-format
- Quotes: `kdf_iterations = 100000` with the comment "was 10000 before PrivateBin version 1.3"; "These bytes encoded in base58 form the fragment part (characters after `#`) of paste url."
- Note: the page defines `paste_passphrase = paste_key + paste_password` (byte concatenation) when a password is set, so the password feeds the same PBKDF2 derivation rather than acting as a separate authentication step.
- Status: VERIFIED.

Storage backend. Flat files by default; relational database, S3 and Google Cloud Storage supported.
- URL: https://github.com/PrivateBin/PrivateBin/wiki/Configuration
- Quotes: "Currently PrivateBin stores its pastes as flat files by default" / "several cloud storage backends are supported, such as S3 with Ceph or AWS and Google Cloud Storage."
- Status: VERIFIED.

Burn after reading, server side. The paste is deleted inside the model's `get()` on the first JSON API read, before the response is built; there is no lock or compare-and-delete in the code read.
- URL: https://raw.githubusercontent.com/PrivateBin/PrivateBin/master/lib/Model/Paste.php
- Quote (code): `if (($data['adata'][self::ADATA_BURN_AFTER_READING] ?? 0) === 1) { $this->delete(); }`
- Status: VERIFIED for "delete on first read"; PARTIAL for atomicity (read-then-delete, no locking seen; the storage layer was not reviewed).

Burn after reading, client side ("click to reveal"). Since 1.7.0 the fragment of a burn-after-reading link carries a leading dash, which lets the page show a confirmation dialog before it requests (and thereby destroys) the paste. The marker is not a security control.
- URL: https://github.com/PrivateBin/PrivateBin/pull/1237
- Quotes: "Ask before loading burn after reading pastes"; the maintainer chose "the less ambiguous dash"; "it doesn't prevent anyone from manually adding it to a regular URL."; the change "has been in release 1.7.0 and 1.7.1."
- Status: VERIFIED.

History of the mechanism. Originally the ciphertext was embedded in the HTML response, so any fetch (including link previews) burned the paste; an interceptor could fetch with cURL and keep the ciphertext.
- URL: https://github.com/PrivateBin/PrivateBin/issues/174
- Quote: "Originally the paste was automatically included in the HTML response when accessing a paste."
- Status: VERIFIED.

Bot handling. Maintainers rely on a user-agent block list plus the fact that previewers must execute JavaScript.
- URL: https://github.com/PrivateBin/PrivateBin/issues/1185
- Quote: "It should not do that. They need to execute JS and we do have a block list included for such bots."
- Status: VERIFIED.

Size limits. 10 MB per paste and comment by default; file upload off by default.
- URL: https://github.com/PrivateBin/PrivateBin/wiki/Configuration and https://raw.githubusercontent.com/PrivateBin/PrivateBin/master/cfg/conf.sample.php
- Quotes: "Size limit per paste and comment in bytes, defaults to 10 Mibibytes (10485760 bytes)." (wiki); `sizelimit = 10000000` "defaults to 10 Megabytes" (sample config; the two documents disagree by about 5%). "The `fileupload` option enables or disables the file upload feature, defaults to false."
- Status: VERIFIED.

Maintenance status. Latest release 2.1.0, published 2026-10-18; minimum PHP 7.4.
- URL: https://api.github.com/repos/PrivateBin/PrivateBin/releases/latest
- Quote: `"tag_name": "2.1.0"`, `"published_at": "2026-10-18T08:38:01Z"`
- Status: VERIFIED.

License. Zlib/libpng for the PrivateBin code, with bundled components under other licenses.
- URL: https://github.com/PrivateBin/PrivateBin/blob/master/LICENSE.md
- Quote: "PrivateBin consists of PHP and JS code which was originally written by Sébastien Sauvage in 2012"
- Status: VERIFIED.

Published advisories (13 on GitHub, two pages).
- URL: https://github.com/PrivateBin/PrivateBin/security/advisories and https://github.com/PrivateBin/PrivateBin/security/advisories?page=2
- Titles as published: "Insufficient Markdown/HTML Sanitization enables Deceptive Popups due to Bootstrap classes" (Oct 2026); "Reflected JSON injection reachable via unescaped request path" (Aug 2026); "Stored XSS due to PDF preview rendering HTML with maliciously forged mime types" (Aug 2026); "Reflected JSON injection in backend responses via unescaped REQUEST_URI" (Jul 2026); "Stored Cross-Side-Scripting (XSS) vulnerability in attachment download link via dangerous MIME types with required user-interaction" (Jul 2026); "Malicious filename can be used for self-XSS / HTML injection locally for users" (Nov 2025); "Template-switching feature allowing arbitrary local file inclusion through path traversal" (Nov 2025); "Missing HTML sanitisation of attached filename in file size hint enabling persistent XSS, defacement, open redirect" (Oct 2025); "Bypass of YOURLS proxy URL filter..." (Jul 2024); "Persistent Cross-site Scripting (XSS) vulnerability in SVG attachments in PrivateBin < v1.4.0" (Apr 2022); "Persistent XSS vulnerability in filename of attached file in PrivateBin < 1.3.2/1.2.2" (Jan 2020); "Vulnerability Report 2018-08-11"; "Vulnerability Report 2017-09-29".
- Status: VERIFIED.

CVE-2022-24833 (SVG attachment XSS). Fixed in 1.4.0 by sanitizing SVG previews with DOMPurify and sending the CSP also as a meta tag; present since v0.21 (ZeroBin era); reported by Ian Budd of Nethemba.
- URL: https://github.com/PrivateBin/PrivateBin/security/advisories/GHSA-cqcc-mm6x-vmvw
- Quotes: "SVGs can contain JavaScript."; the script "cannot access paste content or encryption keys"; DOMPurify "strips script tags and other uncommon security-relevant and potentially malicious tags/properties."
- Scoring: GitHub advisory CVSS 4.3 (moderate); the CVE record scores it 8.2 (high), vector `CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:H/I:L/A:N` (https://cveawg.mitre.org/api/cve/CVE-2022-24833, published 2022-04-11).
- Status: VERIFIED (NVD HTML page returned only "NVD - Home"; the CVE Services API was used instead).

PDF preview XSS (GHSA-326h-xgp6-8jcx). A MIME type such as `text/html /pdf` passed an `endsWith('/pdf')` check and was rendered as HTML; patched in 2.0.6; exploitable only with uploads enabled and CSP disabled or weakened.
- URL: https://github.com/PrivateBin/PrivateBin/security/advisories/GHSA-326h-xgp6-8jcx
- Quotes: "renders any attachment whose attacker-controlled MIME type ends with `/pdf`"; "allowing stored JavaScript execution when the attachment preview loads."
- Status: VERIFIED.

Third-party audit. None found.
- Tried: web search "PrivateBin security audit third-party pentest"; https://www.privacyguides.org/en/pastebins/ (lists "Should have a published audit from a reputable, independent third party." as a criterion but does not say PrivateBin has one).
- Status: NOT FOUND.

What the FAQ says the server can and cannot know.
- URL: https://github.com/PrivateBin/PrivateBin/wiki/FAQ
- Quotes: unencrypted metadata includes the creation timestamp, expiration, format and whether discussions are enabled; "everyone who gets the link can decrypt the contents of a PrivateBin document" unless a password is set; with URL shorteners the key "can be retrieved from the shorteners access logs".
- Status: VERIFIED.

## 2. Yopass

Trust model. Browser-side OpenPGP encryption; key in the URL fragment; server never sees it.
- URL: https://github.com/jhaals/yopass
- Quotes: "The browser encrypts your secret before it reaches the server and the decryption key is never stored with the secret." / "Yopass creates a link whose URL fragment contains the decryption key. URL fragments are not sent to the server."
- Status: VERIFIED.

Storage backend. Memcached (default) or Redis for secrets; disk or S3-compatible storage for files.
- URL: https://github.com/jhaals/yopass
- Quotes: "Redis or Memcached storage" / "Disk and S3-compatible file storage."
- Status: VERIFIED.

One-time semantics and atomicity. Documented as removal on first retrieval. In code, the Redis backend reads inside WATCH/MULTI/EXEC and writes a claim record, treating a failed transaction as not-found; the Memcached backend uses compare-and-swap to replace the value with a claim marker and deliberately never issues a Delete.
- URLs: https://github.com/jhaals/yopass ; https://raw.githubusercontent.com/jhaals/yopass/master/pkg/server/redis.go ; https://raw.githubusercontent.com/jhaals/yopass/master/pkg/server/memcached.go
- Quotes: "A one-time secret is removed after its first retrieval." (README); `if err == redis.TxFailedErr {` (redis.go); "Never follow it with Delete, which could remove a replacement." (memcached.go)
- Status: VERIFIED.

Click to reveal. A confirmation step before revealing one-click secrets was added in September 2025 after reports that email scanners destroyed secrets.
- URL: https://github.com/jhaals/yopass/issues/2154
- Quotes: "GET should not be destructive, so there should be a kind of confirmation to prevent issues with preview fetchers" / "A lot of email antivirus will visit links in emails, and destroy the one-time secret." / owner: "Fixed in the latest release :)" (closed 2025-09-04)
- Status: VERIFIED.

Size limits. The README gives no text limit; file uploads above 1 MB are a paid feature.
- URL: https://github.com/jhaals/yopass
- Quote: business license adds "File uploads larger than 1 MB."
- Status: PARTIAL (text limit not stated on the page).

Maintenance status. 16.0.0 published 2026-10-17, described as a security release.
- URLs: https://api.github.com/repos/jhaals/yopass/releases/latest ; https://github.com/jhaals/yopass/releases/tag/16.0.0 ; https://github.com/jhaals/yopass/releases
- Quotes: `"published_at": "2026-10-17T05:12:58Z"`; "This release addresses 27 security findings"; "15 medium and 12 low severity". The notes also say Redis creation retries can no longer restore consumed one-time secrets, Redis/webhook/storage credentials were removed from logs, responses use no-store caching, decrypted files get explicit binary MIME types, and Argon2 memory and OpenPGP decompression are bounded. 15.1.0 (13 Oct) added "separate link and key sharing".
- Status: VERIFIED.

License. Apache-2.0 for the open source edition; OIDC, theming, audit logging, secret requests, read receipts, webhooks and uploads over 1 MB need a business license.
- URL: https://github.com/jhaals/yopass
- Status: VERIFIED.

Documented caveats.
- URL: https://github.com/jhaals/yopass
- Quotes: "Yopass must be served over HTTPS in production so the web application and encrypted payload cannot be modified in transit." / "The public demo is useful for testing Yopass. Self-host your own instance when sharing sensitive information."
- Status: VERIFIED.

Audit. NOT FOUND (web search "yopass security audit OR pentest report" returned only the project's disclosure policy and unrelated reports).

## 3. OneTimeSecret

Trust model. Server-side encryption. Without a passphrase the server holds everything needed to decrypt; with a passphrase, the passphrase is mixed into the encryption key and only a bcrypt hash of it is stored.
- URLs: https://onetimesecret.com/en/about ; https://docs.onetimesecret.com/
- Quotes: "If you use a passphrase (available under "Privacy Options"), we include that in the encryption key for the secret." / "We only store a bcrypt hash of the passphrase, making it impossible for us to decrypt your secret once it's saved." / "Your secrets are securely encrypted on our servers, ensuring data protection."
- Status: VERIFIED.

Storage backend. Redis; the full self-hosted setup adds PostgreSQL and RabbitMQ.
- URL: https://github.com/onetimesecret/onetimesecret
- Status: VERIFIED (Quick Start runs Redis and points the app at `REDIS_URL`).

One-time semantics and click to view. The service displays the secret once and deletes it. The official docs do not describe the confirmation page; a university knowledge-base article describes it.
- URLs: https://onetimesecret.com/en/about ; https://docs.onetimesecret.com/en/secret-links ; https://it.chicagobooth.edu/TDClient/30/Portal/KB/ArticleDet?ID=4913
- Quotes: "We display it once and then delete it. After that, it's gone forever." / "Each link can only be accessed once before it disappears forever." / "When they click the link they will see the option to view the secret info BUT ONLY ONCE."
- Status: VERIFIED for one-time; PARTIAL for the confirmation step (third-party description only).

Size limits and retention. 100 KB and 7 days for anonymous users; 1000 KB and 14 days for free accounts.
- URL: https://onetimesecret.com/en/about
- Quotes: "Anonymous users can create secrets that last up to 7 days and have a maximum size of 100 KB." / "Free account holders get extended benefits: secrets can last up to 14 days and can be up to 1000 KB in size."
- Status: VERIFIED.

Maintenance status and license. v0.26.18 published 2026-10-17; MIT.
- URLs: https://api.github.com/repos/onetimesecret/onetimesecret/releases/latest ; https://github.com/onetimesecret/onetimesecret
- Quotes: `"tag_name": "v0.26.18"`, `"published_at": "2026-10-17T08:05:53Z"`; "MIT — see LICENSE.txt."
- Status: VERIFIED.

Published advisories (six, all 2026).
- URL: https://github.com/onetimesecret/onetimesecret/security/advisories
- Titles: "Password / Magic Link Reset Poisoning via X-Forwarded-Proto..." (High, Sep 2026); "IPv6 transition address bypass of SSO issuer URL SSRF guard" (Moderate); "API Key Exposed in Unauthenticated Frontend Bootstrap Payload" (High); "Unauthenticated Secret Existence Oracle and Metadata Disclosure via /secret/:id/status with Destructive Side Effect" (Moderate); "V1 API Secret Endpoint Missing PassphraseRateLimiter Enables Unlimited Brute-Force for Authenticated Users" (Moderate); "Passphrase Brute-Force Oracle via `correct_passphrase` Boolean in V2 API Response" (Low).
- Status: VERIFIED.

The status-endpoint advisory in detail. An unauthenticated status endpoint returned full metadata, let attackers enumerate IDs, and changed state on each probe. Affected up to v0.25.11, patched in 0.26.0, CVSS 6.5.
- URL: https://github.com/onetimesecret/onetimesecret/security/advisories/GHSA-rwvh-r24h-rrgg
- Quote: "Every probe also silently transitions the secret from `new` to `previewed`."
- Status: VERIFIED.

Other documented caveats. The README warns that losing the server `SECRET` key makes existing secrets unreadable, and discloses that AI tools helped with architecture, code and docs.
- URL: https://github.com/onetimesecret/onetimesecret
- Status: VERIFIED.

## 4. Bitwarden Send

Encryption model. A 128-bit secret per Send, expanded with HKDF-SHA256 to a 512-bit key, used for AES-256; the key travels in the URL fragment; the password is authentication only and plays no part in encryption.
- URL: https://bitwarden.com/help/send-encryption/
- Quotes: "A new 128-bit secret key is generated for the Send." / "The derived key is used to AES-256 encrypt the send, including its file/text data and metadata." / "The anchor/fragment/hash is not sent to the server." / "Any password used to protect a Send is not involved in the encryption and decryption of a Send." / "Passwords are purely an authentication method."
- Note: the page does not name the AES mode or any MAC, and shows two different link path formats.
- Status: VERIFIED.

Limits. Text Sends up to 1,000 characters; file Sends 500 MB (100 MB on mobile) and premium only.
- URL: https://bitwarden.com/help/create-send/
- Quotes: "The maximum file size per file Send is 500 MB (100 MB on mobile)." / "Only premium users or members of a paid organization (Families, Teams, or Enterprise) can make a file Send."
- Status: VERIFIED.

What counts as an access. Text Sends count link accesses; file Sends count downloads; a view limit is optional; default deletion after seven days, up to 31.
- URL: https://bitwarden.com/help/send-lifespan/
- Quotes: Sends "have a default lifespan of seven days"; "Deletion date has a maximum allowed value of 31 days from creation."
- Status: VERIFIED (the page does not say whether failed password attempts count).

Audits. Bitwarden publishes 25 assessments (Cure53 2018 to 2025, ETH Zurich cryptography report 2025, others). None of the scope descriptions names Send. The 2018 Cure53 report summary does not mention Send. A local scan of the decompressed text streams of the 2021 and 2022 Cure53 PDFs found no plain-text "Send", but the fonts may be CID-encoded, so that result is inconclusive.
- URLs: https://bitwarden.com/help/is-bitwarden-audited/ ; https://pentestreports.com/reports/cure53/pentest-report_bitwarden ; https://bitwarden.com/assets/4G0yonTshy2ezRo1R7s6Yl/7ba5bdac721b2ad8d14117c1c6a36b37/2021-bitwarden-security-assessment-report.pdf ; https://bitwarden.com/assets/4eMmA16Zz9MACTHOexlxx0/05f3ed75c04f7d6e086479279d82c733/2022_Bitwarden_Security_Assessment_Report.pdf
- Quote: the 2025 ETH Zurich entry covers core cryptographic operations "assuming a fully malicious server".
- Status: PARTIAL (Send is not named in any published scope; the PDFs could not be text-extracted reliably).

Vaultwarden Send. Instance-wide switch, per-user storage quota and purge job exist; no per-Send size cap in the template.
- URL: https://raw.githubusercontent.com/dani-garcia/vaultwarden/main/.env.template
- Quotes: "## Controls whether users are allowed to create Bitwarden Sends." (`# SENDS_ALLOWED=true`); "## Max kilobytes of send storage allowed per user." (`# USER_SEND_LIMIT=`); `# SEND_PURGE_SCHEDULE="0 5 * * * *"`
- Status: VERIFIED. (The wiki page `Enabling-Send` does not exist; GitHub showed the "create page" editor.)

## 5. Firefox Send (Mozilla, discontinued)

Repository status and license. Archived 2021-05-22, read-only; MPL 2.0.
- URL: https://github.com/mozilla/send
- Quotes: "This repository is archived." / "Mozilla discontinued the Firefox Send service in September 2021." (the README's wording; press coverage dates the announcement to 17 September 2020, see below)
- Status: VERIFIED.

Crypto design. Random secret key, HKDF-SHA256 to derive a file key (AES-GCM via ECE / RFC 8188), a metadata key (AES-GCM) and an HMAC-SHA256 signing key; key in the fragment; passwords derive a new signing key via PBKDF2 over password plus full URL.
- URL: https://github.com/mozilla/send/blob/master/docs/encryption.md
- Quotes: "A new secret key is generated with `crypto.getRandomValues`" / "The secret key is used to derive more keys via HKDF SHA-256." / "128-bit AES-GCM encryption via the Web Crypto API" / "A new signing key is generated via PBKDF2 from the user entered password and the full share url (including secret key fragment)."
- Gaps: no key length for the random secret, no HKDF salt/info, no PBKDF2 iteration count on the page.
- Status: VERIFIED.

Limits (server defaults). 1 GiB and 5 downloads and 1 day for anonymous users; 2.5 GiB, 100 downloads and 7 days with an account.
- URLs: https://raw.githubusercontent.com/mozilla/send/master/server/config.js ; https://blog.mozilla.org/en/products/firefox/introducing-firefox-send-providing-free-file-transfers-while-keeping-your-personal-information-private/
- Quotes: `anon_max_downloads` "default: 5,"; `max_downloads` "default: 100,"; `anon_max_expire_seconds` "default: 86400,"; "To send files up to 2.5GB, sign up for a free Firefox account."
- Status: VERIFIED.

July 2020 suspension and planned mitigations. Mozilla planned an abuse-reporting mechanism and mandatory Firefox Account sign-in before relaunch.
- URL: https://www.sophos.com/en-us/blog/mozilla-turns-off-firefox-send-following-malware-abuse-reports/
- Quotes (Mozilla statement as reported): "Before relaunching, we will be adding an abuse reporting mechanism to augment the existing Feedback form" / "we will require all users wishing to share content using Firefox Send to sign in with a Firefox Account."
- Status: VERIFIED (secondary source; Mozilla's own page could not be fetched, see list at the end).

The abuse trigger. Amnesty's June 2020 report on spyware against Indian human rights defenders documented Send-hosted payloads.
- URL: https://www.amnesty.org/en/latest/research/2020/06/india-human-rights-defenders-targeted-by-a-coordinated-spyware-operation/
- Quotes: "All these spearphishing emails included a malicious link to a file hosted on Firefox Send" / "We suspect that this technique was used to avoid detection by e-mail spam and malware filters"
- Status: VERIFIED.

September 2020 shutdown statement.
- URL: https://techcrunch.com/2020/09/17/mozilla-shutters-firefox-send-and-notes/
- Quotes (attributed to Mozilla): "Unfortunately, some abusive users were beginning to use Send to distribute malware and as part of spear phishing attacks." / Mozilla "made the decision not to relaunch the service."
- Status: VERIFIED.

Third-party audit of Firefox Send. NOT FOUND. Searches "Firefox Send Cure53 security audit report" and "'Firefox Send' security audit OR 'security review' Mozilla 2019" surfaced only the 2016 Cure53 audit of Firefox Accounts and the 2025 audit of Thunderbird Send.

Audit of the successor (Thunderbird Send, 2025). 7ASecurity with OSTIF; whitebox; one critical and one high finding plus 20 recommendations; report published under CC BY-SA 4.0.
- URL: https://blog.thunderbird.net/2025/12/thunderbird-send-security-audit-with-ostif-and-7asecurity/
- Quotes: "It is built on the foundation of the original Firefox Send project, although much has changed since those days." / one endpoint "had the potential to expose some user data without requiring authentication." / Report: https://7asecurity.com/reports/pentest-report-thunderbird-send-RC1.2.pdf
- Status: VERIFIED.

## 6. Other tools, briefly

### cryptgeon (cupcakearmy)
- Trust model: XChaCha20-Poly1305 on the client; id and key are 256-bit. "the server never sees the encryption key and cannot decrypt the contents of the notes even if it tried to." Whether the key sits in the fragment is not stated in the README. (https://github.com/cupcakearmy/cryptgeon, VERIFIED / PARTIAL for key placement)
- One-time mechanism: view count (`MAX_VIEWS` default 100) or time limit; with `ALLOW_ADVANCED=false` "all notes will be one view only." Known race: "Multiple instances connected to the same cache instance can run into race conditions." (VERIFIED)
- Storage: Redis or Valkey, "stored in memory and never persisted to disk." `SIZE_LIMIT` default 1 KiB, "`512 MiB` is the maximum allowed." (VERIFIED)
- Maintenance: latest GitHub release v2.9.3, 2026-06-25 (https://api.github.com/repos/cupcakearmy/cryptgeon/releases/latest); the README references a `v3` image tag. License MIT. (VERIFIED)
- Caveat quoted: "`https` is required otherwise browsers will not support the cryptographic functions."

### Enclosed (corentinth)
- Trust model: AES-GCM 256 on the client; base key in the URL hash; optional password mixed in via PBKDF2-SHA256 (iteration count not published). "The base key is unique and is not known to the server." The fragment "is never sent to the server, making it a secure way to transmit the key needed for decryption." Security "relies on the secrecy of the link." (https://docs.enclosed.cc/how-it-works, VERIFIED)
- One-time mechanism: "choose to have the note self-destruct after being read"; the docs do not say whether deletion happens on fetch or after decryption. (https://github.com/corentinth/enclosed, PARTIAL)
- Storage and hosting: the public instance runs on Cloudflare Pages with Cloudflare KV; storage goes through Unstorage. Workers are not mentioned. (VERIFIED)
- Maintenance: v1.16.0, 2025-07-22 (https://api.github.com/repos/corentinth/enclosed/releases/latest). License Apache-2.0. No size limit stated. (VERIFIED)

### Hemmelig.app
- Trust model: "Client-side AES-256-GCM encryption"; "All encryption happens in your browser."; "The server only stores encrypted data and never sees your secrets or encryption keys."; links look like `/s/<id>#<key>`. (https://github.com/HemmeligOrg/Hemmelig.app, VERIFIED)
- One-time mechanism: "The secret is automatically deleted after being viewed or expired"; configurable view limits. (VERIFIED)
- Storage: SQLite file in the Docker example; file uploads for authenticated users only. (VERIFIED)
- Maintenance: the repository is archived. "This repository is archived. It is read-only and gets no more fixes, features, or releases." GitHub API: `archived: true`, last push 2026-10-05. The hosted service "continues to run" separately. License: O'Saasy (modified MIT that forbids competing SaaS use). (VERIFIED)

### Password Pusher (pglombardo)
- Trust model: server-side. "Password Pusher does not provide custom client-side encryption for sensitive data." Data at rest uses AES-GCM 256 with per-field keys derived from a master key; "Unlike other sensitive data, file uploads are not encrypted by Password Pusher before storage."; "Access logs are maintained to provide insight into who has viewed each password" (https://docs.pwpush.com/docs/security/, VERIFIED)
- One-time mechanism: expiry after N views and/or time; optional "1-click retrieval step" against scanners (see cross-cutting section). (https://docs.pwpush.com/docs/faq/, VERIFIED)
- Storage: Postgres, MySQL or SQLite, or stateless. Maintenance: v2.15.1, 2026-10-16 (https://api.github.com/repos/pglombardo/PasswordPusher/releases/latest). License Apache-2.0. (VERIFIED)
- Advisories: CVE-2024-51989 / GHSA-5chg-cq29-gfqf, XSS in account confirmation, affects >v1.41.1 and <=v1.48.0, patched v1.48.1, CVSS 7.1: "Users who self-host and have the login system enabled are affected." (https://github.com/pglombardo/PasswordPusher/security/advisories/GHSA-5chg-cq29-gfqf, VERIFIED). CVE-2026-59802 / GHSA-76c2-66pg-fj2f, data: URI accepted in URL pushes, fixed in 2.8.1, CVSS 4.0 6.3 (https://osv.dev/vulnerability/CVE-2026-59802, VERIFIED).

### ots (Luzifer)
- Trust model: "The secret is encrypted with a symmetric 256bit AES encryption in the browser before being sent to the server."; key in the fragment; "Server never receives the plain text secret". (https://github.com/Luzifer/ots, VERIFIED)
- One-time mechanism: "the secret is immediately deleted on the first read." (VERIFIED; no click-to-reveal step described on the page)
- Storage: in-memory ("wiped on restart of the daemon") or Redis; `SECRET_EXPIRY` default 0 (no expiry). Maintenance: v1.23.0, 2026-10-06 (https://api.github.com/repos/Luzifer/ots/releases/latest). License Apache-2.0. (VERIFIED)
- Caveat quoted: do not send plaintext to the API of a public instance; the README recommends encrypting before sending.

### snappass (Pinterest)
- Trust model: server-side Fernet; the key is generated by the server and placed in the URL path, not the fragment, so the server receives it on retrieval. Code: `token = TOKEN_SEPARATOR.join([storage_key, encryption_key])`, `link = base_url + quote_plus(token)`. README: "A random unique key is generated for each password, and is never stored;" / "This means that even if someone has access to the Redis store, the passwords are still safe." (https://raw.githubusercontent.com/pinterest/snappass/master/snappass/main.py ; https://raw.githubusercontent.com/pinterest/snappass/master/README.rst, VERIFIED)
- One-time mechanism: GET on the token shows a preview page that only checks existence; a POST reveals and consumes it in a Redis MULTI/EXEC pipeline (`pipe.get(storage_key)`, `pipe.delete(storage_key)`). (VERIFIED)
- Storage: Redis, default TTL two weeks. Maintenance: no GitHub releases; last push 2026-10-08; license MIT (https://api.github.com/repos/pinterest/snappass). (VERIFIED)
- Caveats quoted: creation sends the password in the body "to prevent the password from being logged in the server logs."; without `HOST_OVERRIDE` the link host comes from the Host header "which a client can spoof."

### Wormhole.app
- Trust model: "Wormhole encrypts all files with **128-bit AES-GCM encryption** before they leave the browser."; "The secret key is added to the URI fragment which is never sent to the server."; RFC 8188 streaming, HKDF-SHA256. (https://wormhole.app/security, VERIFIED)
- Limits: "up to 10GB of local files"; links expire after 24 hours. (https://www.theregister.com/2021/04/09/wormhole_file_transfer/, VERIFIED)
- Audit: the security page cites Mozilla Observatory and SSL Labs grades, not an independent audit. (NOT FOUND)
- Abuse stance: the co-founder said "we may need to introduce client-side virus scanning" if malware becomes an issue. (The Register, VERIFIED)

## 7. Cross-cutting evidence

### (a) Link previews and scanners consuming one-time links

- Yopass, issue #2154 (closed 2025-09-04 with a confirmation page): "A lot of email antivirus will visit links in emails, and destroy the one-time secret." and "Destroying the secret on robot visits degrades the value yopass can provide". https://github.com/jhaals/yopass/issues/2154 VERIFIED.
- PrivateBin, issue #1185 names MS Outlook, MS Teams and Microsoft Defender: "This preview triggers the burn after reading mechanism, even with a password specified." Closed as duplicate of #982 with the answer that previewers "need to execute JS and we do have a block list included for such bots." https://github.com/PrivateBin/PrivateBin/issues/1185 VERIFIED.
- PrivateBin, issue #982 (iMessage suspected; the reporter later wrote "I tested this again in iMessage, and it seemed to work fine, I think."; closed asking for a user agent string to block). https://github.com/PrivateBin/PrivateBin/issues/982 VERIFIED (unreproduced).
- PrivateBin, issue #1219 "Safari burns document during automatic preview from highlighting URL" (bug, closed Feb 2024) and PR #1237, which added the dash marker and confirmation dialog in 1.7.0. https://github.com/PrivateBin/PrivateBin/issues?q=burn+after+reading+preview and https://github.com/PrivateBin/PrivateBin/pull/1237 VERIFIED.
- PrivateBin, issue #174 (2017 to 2018): maintainers note that embedding ciphertext in the HTML meant "link-inspecting services, such as chat apps and social media previews" could destroy pastes; the fix moved loading behind a dialog. https://github.com/PrivateBin/PrivateBin/issues/174 VERIFIED.
- Password Pusher FAQ: "Some systems such as email, firewalls and chat systems often have link scanners that can eat up views." and "To prevent this, use the 1-click retrieval step option when pushing passwords." The FAQ lists WhatsApp, Slack and Teams as scan sources and says a push shared via WhatsApp shows an audit entry labelled "WhatsApp Bot". https://docs.pwpush.com/docs/faq/ VERIFIED.
- Generic evidence outside paste tools: a Jira add-on vendor documents "The most common cause is when the Outlook/O365 Desktop client is used along with ProofPoint SafeLinks." https://thepluginpeople.atlassian.net/wiki/spaces/JEMHC/pages/3856236545/Security+Scanners+consuming+one+time+links VERIFIED.
- Bitwarden Send: no issue found about previews consuming the access count (search "Bitwarden Send access count link preview Slack Teams"). NOT FOUND.

### (b) XSS through rendered attachments or content

- PrivateBin SVG attachment preview, CVE-2022-24833 (see section 1). VERIFIED.
- PrivateBin PDF preview with forged MIME type, GHSA-326h-xgp6-8jcx, patched 2.0.6 (see section 1). VERIFIED.
- PrivateBin attachment download link with dangerous MIME types (GHSA-f2xf-7x3g-4272, Jul 2026) and three filename-based XSS/HTML injection advisories (Jan 2020, Oct 2025, Nov 2025), plus a Markdown/Bootstrap-class sanitization issue (Oct 2026). https://github.com/PrivateBin/PrivateBin/security/advisories VERIFIED.
- Password Pusher: CVE-2024-51989 (account confirmation XSS) and CVE-2026-59802 (data: URI in URL pushes). VERIFIED.
- Hemmelig, cryptgeon, Enclosed: no advisories surfaced by search. NOT FOUND.

### (c) Abuse of anonymous sharing for malware and phishing

- Firefox Send: Amnesty documented Send-hosted spyware in spear-phishing; Mozilla suspended the service in July 2020, planned abuse reporting and mandatory accounts, then shut it down in September 2020 (section 5). VERIFIED.
- Wormhole.app launched in 2021 with a 24-hour retention and no accounts; its co-founder said "we may need to introduce client-side virus scanning". https://www.theregister.com/2021/04/09/wormhole_file_transfer/ VERIFIED.
- PrivateBin's FAQ frames the operator's position as not holding keys, so content cannot be inspected server-side; file upload is off by default. https://github.com/PrivateBin/PrivateBin/wiki/FAQ VERIFIED.

### (d) Published third-party audits

- Bitwarden: 25 published assessments (Cure53 2018 to 2025, ETH Zurich, Unit 42, Fracture Labs, IOActive, Mandiant, PIE); none names Send in scope. https://bitwarden.com/help/is-bitwarden-audited/ PARTIAL (Send not named; PDFs not text-extractable here).
- Thunderbird Send (successor to Firefox Send): 7ASecurity with OSTIF, December 2025. https://blog.thunderbird.net/2025/12/thunderbird-send-security-audit-with-ostif-and-7asecurity/ VERIFIED.
- PrivateBin, Yopass, OneTimeSecret, cryptgeon, Enclosed, Hemmelig, Password Pusher, ots, snappass, Wormhole: no published independent audit found. NOT FOUND.

### (e) Key leakage through Referer, analytics or logs

- PrivateBin FAQ warns that a URL shortener that receives the full URL (with the key) can log it: it "can be retrieved from the shorteners access logs". https://github.com/PrivateBin/PrivateBin/wiki/FAQ VERIFIED.
- snappass puts the Fernet key in the URL path, so the retrieval request carries it to the server (and server logs), unlike fragment-based tools. https://raw.githubusercontent.com/pinterest/snappass/master/snappass/main.py VERIFIED.
- Element Call (CVE-2026-48007, GHSA-6vhh-4xw6-h2h2): analytics sent `$current_url` including the fragment, so the standalone app may have reported "including encryption passwords, to the configured PostHog server." Fixed in 0.19.4. https://github.com/advisories/GHSA-6vhh-4xw6-h2h2 VERIFIED (not a paste tool, but the same key-in-fragment design).
- Guzzle (CVE-2026-67354, GHSA-h95v-h523-3mw8): with the `referer` option on, "the redirect destination can receive a `Referer` ending in `#secret`"; fixed in 7.15.1. Relevant to server-side fetchers that follow redirects on secret URLs. https://github.com/advisories/GHSA-h95v-h523-3mw8 VERIFIED.
- Boston University study of 21 collaboration services: "seven of them are vulnerable to this kind of secret information disclosure." via Referer, "because they do not implement a strict enough and up-to-date policy." https://agile.bu.edu/fw/SOS/LeakingReferers VERIFIED.
- Yopass 16.0.0 removed Redis, webhook and storage credentials from logs and stopped sampling audit logs. https://github.com/jhaals/yopass/releases/tag/16.0.0 VERIFIED.
- OneTimeSecret's status endpoint leaked metadata and existence to anonymous callers and mutated state on each probe (GHSA-rwvh-r24h-rrgg). VERIFIED.

## 8. What the tools say they can and cannot guarantee

- PrivateBin: "As a user you have to trust the server administrator not to inject any malicious code." and "A server admin can be forced to hand over access logs to the authorities." (https://github.com/PrivateBin/PrivateBin) VERIFIED.
- PrivateBin FAQ: "everyone who gets the link can decrypt the contents of a PrivateBin document" unless a password is set. (https://github.com/PrivateBin/PrivateBin/wiki/FAQ) VERIFIED.
- Yopass: "Yopass must be served over HTTPS in production so the web application and encrypted payload cannot be modified in transit." (https://github.com/jhaals/yopass) VERIFIED.
- Password Pusher: "Password Pusher does not provide custom client-side encryption for sensitive data." and "Unlike other sensitive data, file uploads are not encrypted by Password Pusher before storage." (https://docs.pwpush.com/docs/security/) VERIFIED.
- OneTimeSecret: "We can't access your information even if we wanted to (which we don't)." sits next to "we include that in the encryption key for the secret." about passphrases; without a passphrase the server holds the key. The docs page uses hedged wording: "A secret is designed to be viewed once and then permanently destroyed." (https://onetimesecret.com/en/about ; https://docs.onetimesecret.com/en/security) VERIFIED.
- Enclosed: the link's security "relies on the secrecy of the link." (https://docs.enclosed.cc/how-it-works) VERIFIED.
- cryptgeon: "the server never sees the encryption key and cannot decrypt the contents of the notes even if it tried to." plus the multi-instance race warning. (https://github.com/cupcakearmy/cryptgeon) VERIFIED.
- ots: the server "will never be able to decrypt the secrets it delivers with a reasonable effort." (https://github.com/Luzifer/ots) VERIFIED.
- Bitwarden Send: "Passwords are purely an authentication method." (https://bitwarden.com/help/send-encryption/) VERIFIED.
- Wormhole: "We can't see your Wormhole files, so we can't use them, share them, or sell them." with no disclaimer section. (https://wormhole.app/security) VERIFIED.
- PrivateBin advisory on SVG XSS states the limit of a client-side XSS in a zero-knowledge design: the script "cannot access paste content or encryption keys" but can read same-origin cookies and local storage. (https://github.com/PrivateBin/PrivateBin/security/advisories/GHSA-cqcc-mm6x-vmvw) VERIFIED.

## 9. Design lessons

1. Never let a GET destroy a secret. Serve a non-destructive landing page and consume the secret only on an explicit POST or click. Yopass added this in 2025 after scanner reports (https://github.com/jhaals/yopass/issues/2154); PrivateBin moved to it after issue #174 and PR #1237; snappass and Password Pusher ship it (preview route; "1-click retrieval step").
2. Make the one-time claim atomic in the store. Yopass uses WATCH/MULTI plus a claim record in Redis and CAS in Memcached (https://raw.githubusercontent.com/jhaals/yopass/master/pkg/server/redis.go); snappass uses MULTI/EXEC; cryptgeon documents a race across instances (https://github.com/cupcakearmy/cryptgeon); PrivateBin's model does read-then-delete without a visible lock.
3. Do not treat a "burn" marker in the URL as a security control; it is a UX hint that anyone can add or remove ("it doesn't prevent anyone from manually adding it to a regular URL.", https://github.com/PrivateBin/PrivateBin/pull/1237).
4. Keep the key in the fragment, never in the path or query. snappass puts the Fernet key in the path, so the retrieval request carries it (https://raw.githubusercontent.com/pinterest/snappass/master/snappass/main.py); PrivateBin warns that URL shorteners log it (https://github.com/PrivateBin/PrivateBin/wiki/FAQ).
5. Strip fragments before any analytics, error reporting or logging call; Element Call shipped call passwords to PostHog through `$current_url` (https://github.com/advisories/GHSA-6vhh-4xw6-h2h2).
6. Set a strict Referrer-Policy and never render user-controlled links on the secret page without it; seven of 21 collaboration services leaked secret URLs through Referer (https://agile.bu.edu/fw/SOS/LeakingReferers).
7. Treat attachments as untrusted bytes: never preview SVG, PDF or anything whose MIME type the uploader controls; force a safe MIME type and `Content-Disposition: attachment`; PrivateBin had SVG, PDF, download-link and filename XSS advisories between 2020 and 2026 (https://github.com/PrivateBin/PrivateBin/security/advisories).
8. Ship a Content-Security-Policy and verify it survives the reverse proxy or CDN; the SVG XSS was exploitable only where "the instance isn't protected by an appropriate content security policy" (https://cveawg.mitre.org/api/cve/CVE-2022-24833).
9. Make status and metadata endpoints read-only and rate-limited, and return uniform answers to anonymous callers; OneTimeSecret's status endpoint enumerated IDs, leaked metadata and changed state per probe (https://github.com/onetimesecret/onetimesecret/security/advisories/GHSA-rwvh-r24h-rrgg).
10. Decide whether a password is authentication or key material, and say so. Bitwarden: "Passwords are purely an authentication method." (https://bitwarden.com/help/send-encryption/); PrivateBin and Enclosed feed it into the KDF. Either way, rate-limit it and avoid boolean oracles (OneTimeSecret GHSA-2ww9-v643-gmm2, GHSA-f5gr-cgc7-ffj9).
11. Give a passphrase-protected or "link and key sent separately" mode, since scanners cannot supply a second factor; Yopass 15.1.0 added "separate link and key sharing" (https://github.com/jhaals/yopass/releases).
12. Plan for abuse before launch: anonymous file sharing with no reporting path was used for spear-phishing payloads (https://www.amnesty.org/en/latest/research/2020/06/india-human-rights-defenders-targeted-by-a-coordinated-spyware-operation/) and Mozilla's planned fixes were abuse reporting and mandatory sign-in (https://www.sophos.com/en-us/blog/mozilla-turns-off-firefox-send-following-malware-abuse-reports/). Short retention, download caps (Firefox Send: 5 downloads and 1 day for anonymous users, https://raw.githubusercontent.com/mozilla/send/master/server/config.js) and an abuse-report endpoint are cheap to add early.
13. State the trust boundary plainly in the README, including that the operator can serve malicious JavaScript and may hold access logs (https://github.com/PrivateBin/PrivateBin), and add `no-store` caching and binary MIME types for decrypted downloads (Yopass 16.0.0, https://github.com/jhaals/yopass/releases/tag/16.0.0).
14. Treat one-time secret storage as volatile but idempotent: retries must not resurrect a consumed secret (Yopass 16.0.0: "Redis creation retries can no longer restore consumed one-time secrets").
15. Do not claim an audit you do not have; of the tools studied only Bitwarden (without Send named in scope) and Thunderbird Send publish independent reports (https://bitwarden.com/help/is-bitwarden-audited/ ; https://blog.thunderbird.net/2025/12/thunderbird-send-security-audit-with-ostif-and-7asecurity/).

## 10. URLs opened

Opened and used:
- https://github.com/PrivateBin/PrivateBin
- https://github.com/PrivateBin/PrivateBin/security/advisories
- https://github.com/PrivateBin/PrivateBin/security/advisories?page=2
- https://github.com/PrivateBin/PrivateBin/security/advisories/GHSA-cqcc-mm6x-vmvw
- https://github.com/PrivateBin/PrivateBin/security/advisories/GHSA-326h-xgp6-8jcx
- https://github.com/PrivateBin/PrivateBin/wiki/Encryption-format
- https://github.com/PrivateBin/PrivateBin/wiki/FAQ
- https://github.com/PrivateBin/PrivateBin/wiki/Configuration
- https://github.com/PrivateBin/PrivateBin/releases
- https://github.com/PrivateBin/PrivateBin/releases/tag/2.1.0
- https://api.github.com/repos/PrivateBin/PrivateBin/releases/latest
- https://github.com/PrivateBin/PrivateBin/blob/master/LICENSE.md
- https://github.com/PrivateBin/PrivateBin/issues?q=burn+after+reading+preview
- https://github.com/PrivateBin/PrivateBin/issues/1185
- https://github.com/PrivateBin/PrivateBin/issues/982
- https://github.com/PrivateBin/PrivateBin/issues/174
- https://github.com/PrivateBin/PrivateBin/pull/1237
- https://raw.githubusercontent.com/PrivateBin/PrivateBin/master/lib/Controller.php
- https://raw.githubusercontent.com/PrivateBin/PrivateBin/master/lib/Model/Paste.php
- https://raw.githubusercontent.com/PrivateBin/PrivateBin/master/cfg/conf.sample.php
- https://cveawg.mitre.org/api/cve/CVE-2022-24833
- https://www.privacyguides.org/en/pastebins/
- https://github.com/jhaals/yopass
- https://github.com/jhaals/yopass/issues?q=preview
- https://github.com/jhaals/yopass/issues/2154
- https://github.com/jhaals/yopass/releases
- https://github.com/jhaals/yopass/releases/tag/16.0.0
- https://api.github.com/repos/jhaals/yopass/releases/latest
- https://raw.githubusercontent.com/jhaals/yopass/master/pkg/server/server.go
- https://raw.githubusercontent.com/jhaals/yopass/master/pkg/server/redis.go
- https://raw.githubusercontent.com/jhaals/yopass/master/pkg/server/memcached.go
- https://github.com/onetimesecret/onetimesecret
- https://github.com/onetimesecret/onetimesecret/releases
- https://api.github.com/repos/onetimesecret/onetimesecret/releases/latest
- https://github.com/onetimesecret/onetimesecret/security/advisories
- https://github.com/onetimesecret/onetimesecret/security/advisories/GHSA-rwvh-r24h-rrgg
- https://docs.onetimesecret.com/
- https://docs.onetimesecret.com/en/security
- https://docs.onetimesecret.com/en/secret-links
- https://onetimesecret.com/en/about
- https://it.chicagobooth.edu/TDClient/30/Portal/KB/ArticleDet?ID=4913
- https://bitwarden.com/help/send-encryption/
- https://bitwarden.com/help/create-send/
- https://bitwarden.com/help/send-lifespan/
- https://bitwarden.com/help/is-bitwarden-audited/
- https://bitwarden.com/help/is-bitwarden-audited.md
- https://pentestreports.com/reports/cure53/pentest-report_bitwarden
- https://bitwarden.com/assets/4G0yonTshy2ezRo1R7s6Yl/7ba5bdac721b2ad8d14117c1c6a36b37/2021-bitwarden-security-assessment-report.pdf (binary; local text scan inconclusive)
- https://bitwarden.com/assets/4eMmA16Zz9MACTHOexlxx0/05f3ed75c04f7d6e086479279d82c733/2022_Bitwarden_Security_Assessment_Report.pdf (binary; local text scan inconclusive)
- https://raw.githubusercontent.com/dani-garcia/vaultwarden/main/.env.template
- https://github.com/mozilla/send
- https://github.com/mozilla/send/blob/master/docs/encryption.md
- https://raw.githubusercontent.com/mozilla/send/master/server/config.js
- https://blog.mozilla.org/en/products/firefox/introducing-firefox-send-providing-free-file-transfers-while-keeping-your-personal-information-private/
- https://techcrunch.com/2020/09/17/mozilla-shutters-firefox-send-and-notes/
- https://www.sophos.com/en-us/blog/mozilla-turns-off-firefox-send-following-malware-abuse-reports/
- https://www.hackread.com/mozilla-shuts-down-notes-send-over-malicious-use/
- https://alternativeto.net/news/2020/9/mozilla-shuts-down-firefox-send-encrypted-file-share-service-due-to-malware-phishing
- https://www.amnesty.org/en/latest/research/2020/06/india-human-rights-defenders-targeted-by-a-coordinated-spyware-operation/
- https://blog.thunderbird.net/2025/12/thunderbird-send-security-audit-with-ostif-and-7asecurity/
- https://github.com/cupcakearmy/cryptgeon
- https://api.github.com/repos/cupcakearmy/cryptgeon/releases/latest
- https://github.com/corentinth/enclosed
- https://docs.enclosed.cc/
- https://docs.enclosed.cc/how-it-works
- https://api.github.com/repos/corentinth/enclosed/releases/latest
- https://github.com/HemmeligOrg/Hemmelig.app
- https://api.github.com/repos/HemmeligOrg/Hemmelig.app
- https://github.com/pglombardo/PasswordPusher
- https://docs.pwpush.com/docs/security/
- https://docs.pwpush.com/docs/faq/
- https://github.com/pglombardo/PasswordPusher/security/advisories/GHSA-5chg-cq29-gfqf
- https://osv.dev/vulnerability/CVE-2026-59802
- https://api.github.com/repos/pglombardo/PasswordPusher/releases/latest
- https://github.com/Luzifer/ots
- https://api.github.com/repos/Luzifer/ots/releases/latest
- https://github.com/pinterest/snappass
- https://raw.githubusercontent.com/pinterest/snappass/master/snappass/main.py
- https://raw.githubusercontent.com/pinterest/snappass/master/README.rst
- https://api.github.com/repos/pinterest/snappass
- https://wormhole.app/security
- https://www.theregister.com/2021/04/09/wormhole_file_transfer/
- https://thepluginpeople.atlassian.net/wiki/spaces/JEMHC/pages/3856236545/Security+Scanners+consuming+one+time+links
- https://github.com/advisories/GHSA-6vhh-4xw6-h2h2
- https://github.com/advisories/GHSA-h95v-h523-3mw8
- https://agile.bu.edu/fw/SOS/LeakingReferers

Tried and could not use:
- https://nvd.nist.gov/vuln/detail/CVE-2022-24833 (page body returned only "NVD - Home"; the CVE Services API was used instead)
- https://support.mozilla.org/en-US/kb/what-happened-firefox-send (page failed to load its content)
- https://web.archive.org/web/20201001000000/https://send.firefox.com/ (fetch tool cannot access web.archive.org)
- https://www.zdnet.com/article/mozilla-suspends-firefox-send-service-while-it-addresses-malware-abuse/ (fetch tool cannot access zdnet.com)
- https://www.ghacks.net/2020/07/08/mozilla-suspends-firefox-send-service-temporarily/ (HTTP 403)
- https://www.techradar.com/news/mozilla-suspends-firefox-send-service-following-malware-abuse (article body truncated to navigation)
- https://onetimesecret.com/about (redirect notice only; /en/about used instead)
- https://docs.pwpush.com/docs/retrieval-step/ (HTTP 404; the FAQ covers the feature)
- https://oregonstate.teamdynamix.com/TDClient/1935/Portal/KB/ArticleDet?ID=138091 (HTTP 487)
- https://github.com/dani-garcia/vaultwarden/wiki/Enabling-Send (page does not exist; the env template was used)
- Web searches that returned nothing usable: "PrivateBin security audit third-party pentest"; "Firefox Send Cure53 security audit report"; "'Firefox Send' security audit OR 'security review' Mozilla 2019"; "yopass security audit OR pentest report"; "Bitwarden Send 'access count' link preview Slack Teams"; "Wormhole.app abuse malware phishing" (only look-alike crypto phishing domains, not the service).
