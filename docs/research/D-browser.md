# D. Browser platform facts for a one-time encrypted link service

Research date: 2026-10-21. Every item lists the claim, the page that was opened, a short quote copied from that page, and a status (VERIFIED, PARTIAL, NOT FOUND). Search snippets alone never count as verification; where a page could not be opened, the status says so.

## 1. URL fragment handling

- Claim: the fragment is separated from the URI before dereferencing and is handled only by the user agent.
  URL: https://www.rfc-editor.org/rfc/rfc3986#section-3.5
  Quote: "the fragment identifier is separated from the rest of the URI prior to a dereference" and "dereferenced solely by the user agent".
  Status: VERIFIED.

- Claim: the HTTP target URI excludes the fragment, so it is never in the request line.
  URL: https://www.rfc-editor.org/rfc/rfc9110.html#section-7.1
  Quote: "The target URI excludes the reference's fragment component, if any,"
  Status: VERIFIED.

- Claim: the Referer header never carries the fragment (HTTP semantics).
  URL: https://www.rfc-editor.org/rfc/rfc9110.html#section-10.1.3
  Quote: "A user agent MUST NOT include the fragment and userinfo components of the URI reference"
  Status: VERIFIED.

- Claim: the Referrer Policy "strip url for use as a referrer" algorithm removes the fragment regardless of policy; the default policy is strict-origin-when-cross-origin.
  URL: https://w3c.github.io/webappsec-referrer-policy/#strip-url
  Quote: "Set url's fragment to null." and "The default referrer policy is "strict-origin-when-cross-origin"."
  Status: VERIFIED.

- Claim: WHATWG Fetch excludes the fragment when serializing URLs for the request.
  URL: https://fetch.spec.whatwg.org/
  Quote: "Return the serialization of url with exclude fragment set to true." (serialize a response URL for reporting)
  Status: PARTIAL. Searched three 100k-character chunks of the spec. "exclude fragment" appears only for reporting serialization and for "compute the total request length". The HTTP-network fetch step defers to HTTP semantics ("making an HTTP request over connection"), so the normative statement is RFC 9110 section 7.1 above.

- Claim: Referrer-Policy: no-referrer removes the Referer header entirely.
  URL: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Referrer-Policy
  Quote: "The Referer header will be omitted: sent requests do not include any referrer information."
  Status: VERIFIED.

- Claim: browser session history stores the full URL including the fragment, and a fragment navigation creates a new entry.
  URL: https://html.spec.whatwg.org/multipage/browsing-the-web.html#session-history-entry
  Quote: "A session history entry is a struct with the following items." ... "URL, a URL." and, in Navigate to a fragment, "Let historyEntry be a new session history entry, with"
  Status: VERIFIED.

- Claim: Chrome Sync can upload browsing history to the Google Account.
  URL: https://support.google.com/chrome/answer/165139
  Quote: "You can also sync your tabs and browsing history to your Google Account once you've signed in to Chrome."
  Status: VERIFIED.

- Claim: Chrome synced data is end-to-end encrypted only when the user sets a sync passphrase.
  URL: https://support.google.com/chrome/answer/1181035
  Quote: "With a passphrase, you can use Google's cloud to store your Chrome data without letting Google read it." Also: "Use your own passphrase to encrypt all the Chrome data in your Google Account."
  Status: PARTIAL. The passphrase statement is verified. The page does not state outright that Google can read history without a passphrase; it says only that "your synced data is always protected by industry-leading encryption", which is not an end-to-end claim. The Chrome privacy whitepaper and the Chrome privacy notice pages did not render text (the notice says "The Chrome Privacy Notice is no longer available.").

- Claim: Google uses synced Chrome history for personalization when Web & App Activity is on.
  URL: https://support.google.com/chrome/answer/185277
  Quote: "If you turn on Web & App Activity and sync your Chrome history"
  Status: VERIFIED.

- Claim: Firefox Sync encrypts data on the device with a key derived from the account password; Mozilla cannot read it.
  URL: https://hacks.mozilla.org/2018/11/firefox-sync-privacy/
  Quote: "Firefox Sync by default protects all your synced data so Mozilla can't read it." and "the encryption key derived from your passphrase never leaves your computer."
  Status: VERIFIED (official Mozilla engineering blog). The support.mozilla.org article "How Firefox Sync keeps your data safe even if TLS fails" exists but the page only returned a "required part of the site couldn't load" message, so it was not quotable.

- Claim: a Chrome extension with the "tabs" permission can read the tab URL (which is the full URL; the fragment is part of a URL).
  URL: https://developer.chrome.com/docs/extensions/reference/api/tabs
  Quote: "grants an extension the ability to call tabs.query() against four sensitive properties" ... "url, pendingUrl, title, and favIconUrl."
  Status: VERIFIED for "url"; PARTIAL for the word "fragment", which the docs do not mention separately. Host permissions give the same access: "Host permissions allow an extension to read and query a matching tab's four sensitive tabs.Tab properties." (https://developer.chrome.com/docs/extensions/develop/concepts/declare-permissions says the same.)

- Claim: any script running in the page can read the fragment.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Location/hash
  Quote: "a string containing a "#" followed by the fragment identifier of the location URL."
  Status: VERIFIED.

## 2. Link unfurlers and link scanners

- Slack. Server-side fetch, documented UA, metadata-only fetch, 30-minute cache. JavaScript execution: not stated.
  URL: https://api.slack.com/robots and https://docs.slack.dev/messaging/unfurling-links-in-messages/
  Quote: "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"; "fetches as little of the page as it can (using HTTP Range headers) to extract meta tags about the content"; "Our servers must fetch every URL in a message to determine what kind of content it references."
  Status: VERIFIED (server-side, UA, timing); PARTIAL on JS (undocumented).

- Meta (Facebook, Instagram, Messenger). Server-side crawler with a documented UA. JavaScript execution: not stated.
  URL: https://developers.facebook.com/docs/sharing/webmasters/web-crawlers/
  Quote: "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"; crawls content "shared on one of Meta's family of apps, such as Facebook, Instagram, or Messenger."
  Status: VERIFIED (UA, trigger); PARTIAL on JS.

- WhatsApp. HTTP GET on behalf of the sender; UA encodes the sender's platform. Where the request originates (device or server) and JS execution: not stated.
  URL: https://developers.facebook.com/documentation/business-messaging/whatsapp/link-previews/
  Quote: "WhatsApp crawls the web page via an HTTP GET request."; "The request will have the User-Agent header set to WhatsApp/2.x.x.x A|I|N"; "It's not guaranteed to work (and continue to work)."
  Status: VERIFIED (GET, UA); PARTIAL on origin and JS.

- Telegram. No official documentation found.
  URL: https://bugs.telegram.org/c/65828 (user bug report, weak evidence)
  Quote: "requests with TelegramBot User-Agent return HTTP 200."
  Status: NOT FOUND (official). Tried: web search for the "TelegramBot (like TwitterBot)" UA; only third-party crawler directories and user bug reports exist.

- Discord. Official help article exists but returned HTTP 403 to both fetch attempts.
  URL: https://support.discord.com/hc/en-us/articles/42500550752919-About-Discord-Link-Previews-and-the-Discordbot
  Quote (from the search index of that article, not from an opened page): "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)"
  Status: PARTIAL. Server-side fetch and the UA token appear in the official article's indexed text; the page could not be opened.

- iMessage / Apple Messages. Does not run JavaScript; follows server redirects only.
  URL: https://developer.apple.com/documentation/technotes/tn3156-create-rich-previews-for-messages (content read through Apple's documentation JSON endpoint https://developer.apple.com/tutorials/data/documentation/technotes/tn3156-create-rich-previews-for-messages.json)
  Quote: "Link previews do not follow meta redirects, nor run JavaScript; metadata must be available directly on the linked page."
  Status: VERIFIED (no JS). Which device fetches: NOT FOUND in Apple docs; third-party reports say the sender's device with a UA ending in "facebookexternalhit/1.1 Facebot Twitterbot/1.0" (weak).

- Microsoft Teams (personal). Cloud-backed service fetches the URL while the user types; URLs can be sent to Bing.
  URL: https://support.microsoft.com/en-us/office/how-to-update-your-connected-service-settings-893f468a-4e7e-4f33-9e53-bda8ad0f8fcc
  Quote: "URL service will make a request to the service URL as the user is typing the message."; "Any URLs you provide to Microsoft Teams while using these services can be sent to Microsoft Bing"
  Status: VERIFIED (server-side fetch before sending). UA "SkypeUriPreview": PARTIAL, third-party directories only; no Microsoft Learn page documents it. JS execution: not stated.

- Google Chat. Google documents previews only as a Chat-app feature; no built-in crawler or UA is documented.
  URL: https://developers.google.com/workspace/add-ons/chat/preview-links
  Quote: "only the link URL is sent to the Chat app by link previews."
  Status: NOT FOUND for a Google Chat crawler/UA. Related, verified: Google Messages has a documented fetcher that ignores robots.txt.
  URL: https://developers.google.com/crawling/docs/crawlers-fetchers/google-user-triggered-fetchers
  Quote: "GoogleMessages" ... "is used to generate link previews for URLs sent in chat messages."; "Because the fetch was requested by a user, these fetchers generally ignore robots.txt rules."
  Status: VERIFIED.

- Googlebot renders JavaScript.
  URL: https://developers.google.com/search/docs/crawling-indexing/javascript/javascript-seo-basics
  Quote: "a headless Chromium renders the page and executes the JavaScript."
  Status: VERIFIED. Relevant because X-Robots-Tag: noindex does not stop fetching or rendering; only a robots.txt disallow stops Googlebot, and user-triggered fetchers ignore even that.

- Microsoft Defender for Office 365 Safe Links: scans at delivery, verifies at click, detonates unknown URLs.
  URL: https://learn.microsoft.com/en-us/defender-office-365/safe-links-about
  Quote: "URL scanning and rewriting of inbound email messages during mail flow, and time-of-click verification of URLs"; "As long as Safe Links protection is turned on, URLs are scanned prior to message delivery, regardless of whether the URLs are rewritten or not."; "URLs that don't have a valid reputation are detonated asynchronously in the background."; "Wait for URL scanning to complete before delivering the message"
  Status: VERIFIED. Microsoft does not document whether detonation executes JavaScript or follows in-page buttons; "detonated" denotes sandbox analysis. Also verified: Safe Links rewrites URLs to safelinks.protection.outlook.com and "If a users clicks a rewritten link, it's scanned for malicious content before directing the user to the page." Nothing on the page addresses the fragment.

- Gmail click-time link protection.
  URL: https://support.google.com/mail/answer/10173182?hl=en
  Quote: "clicking a link in a recent message starts a malicious link check."; "The only information logged in connection with this feature is what's needed to run and maintain the service"
  Status: PARTIAL. Click-time checking is documented; whether Gmail fetches links at delivery is not stated on this page.

Conclusion for question 2. It is not safe to assume unfurlers never execute JavaScript. Apple documents that Messages does not; Google documents that Googlebot does; Slack, Meta, WhatsApp, Teams and Discord say nothing either way; Safe Links detonates URLs in a sandbox, which is exactly the case where a page's JavaScript may run and a scripted click may happen. What the sources do support: every documented unfurler fetches the page with a plain GET, most from vendor servers, and most within seconds of the message being posted. A GET of the link must therefore never consume the message. A POST-only consumption step is a reliable defence against every unfurler that only reads HTML. It is a heuristic defence against JS-executing scanners (Googlebot, Safe Links detonation, unknown sandboxes): those can run page scripts, so the POST must be issued only after a user gesture (a click on a reveal control) and should carry a client-generated token, and the server should also refuse requests that carry Sec-Purpose or well-known crawler UAs. None of this excludes a sandbox that clicks buttons; nothing documented lets a page detect such a sandbox.

## 3. Prefetch and prerender

- Claim: prerender executes JavaScript; prefetch fetches only the response body.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Speculation_Rules_API
  Quote: "This includes loading all subresources, running all JavaScript, and even loading subresources and performing data fetches started by JavaScript." and, for prefetch, "download the response body of the referenced pages, but none of the subresources".
  Status: VERIFIED.

- Claim: prerender and prefetch requests carry Sec-Purpose, and any non-2xx response cancels them.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Speculation_Rules_API
  Quote: "Sec-Purpose: prefetch;prerender"; "If a non-success response code is returned (any HTTP status other than in the 200-299 range after redirects), then the page will not be prefetched/prerendered."
  Status: VERIFIED. Chrome's own guidance: "returning a non-2XX response code (such as 503) is the best way to ensure it won't happen." (https://developer.chrome.com/docs/web-platform/prerender-pages)

- Claim: document.prerendering and the prerenderingchange event let the page defer work until activation; PerformanceNavigationTiming.activationStart tells after the fact.
  URL: https://developer.chrome.com/docs/web-platform/prerender-pages and https://developer.mozilla.org/en-US/docs/Web/API/Document/prerendering
  Quote: "The document.prerendering API will return true while the page is prerendering."; "the prerenderingchange event will be dispatched on the document"; MDN: "This feature is not Baseline because it does not work in some of the most widely-used browsers."
  Status: VERIFIED (Chromium-only API).

- Claim: Chrome prerenders URLs typed into the address bar when confidence is high, using the same mechanism as Speculation Rules, and only when "Preload pages" is on.
  URL: https://developer.chrome.com/docs/web-platform/prerender-pages and https://developer.mozilla.org/en-US/docs/Web/Performance/Guides/Speculative_loading
  Quote: "For a greater than 50% confidence level (shown in green), Chrome will prerender the URL."; "Prerender is only enabled for those Chrome users with the "Preload pages" setting in chrome://settings/performance/."; MDN: "Chrome, for example, automatically prerenders pages when matching strings are typed into the address bar"
  Status: VERIFIED. Chrome help adds: "To preload pages, Chrome may use cookies, if you allow cookies" (https://support.google.com/chrome/answer/1385029). The Standard/Extended wording was not on the page that loaded.

- Claim: prerender via Speculation Rules is same-origin by default; cross-origin same-site needs Supports-Loading-Mode: credentialed-prerender; cross-site is not possible.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Speculation_Rules_API and https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Supports-Loading-Mode
  Quote: "Cross-site prerendering is not possible at this time."; "credentialed-prerender: Indicates that a destination origin opts in to loading documents via cross-origin, same-site prerendering."
  Status: VERIFIED. So a chat app or mail client cannot trigger a prerender of your page through Speculation Rules; the realistic prerender sources are your own site and the Chrome address bar.

- Claim: MDN lists one-time and allowance-consuming URLs as unsafe to prefetch/prerender.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Speculation_Rules_API
  Quote: "URLs that increment a user's usage allowance numbers, such as consuming their monthly free article allowance"; "Sign-in flow URLs where the server causes an SMS to be sent"
  Status: VERIFIED.

- Claim: Safari "Preload Top Hit" loads the top search hit in the background, based on bookmarks and history.
  URL: https://support.apple.com/guide/safari/sfria1042d31/mac
  Quote: "Start to load a webpage as soon as it's determined to be a top search hit based on your bookmarks and browsing history."
  Status: VERIFIED for the behaviour; PARTIAL on JS execution, which Apple does not document. It only applies to URLs Safari already knows from bookmarks or history.

- Claim: Firefox network prefetch (link rel=prefetch) is a fetch with Sec-Purpose: prefetch and no rendering.
  URL: https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Attributes/rel/prefetch
  Quote: "functionally equivalent to a fetch() call with a priority: "low" option set on it" ... "it will have a Sec-Purpose: prefetch header set on the request."
  Status: VERIFIED. MDN's Sec-Purpose page adds that Firefox sends the header ("At time of writing Firefox incorrectly sets the Accept header as Accept: */* for prefetches.", https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Sec-Purpose).

- Claim: Speculation Rules support outside Chromium is limited.
  URL: https://caniuse.com/mdn-html_elements_script_type_speculationrules
  Quote: Chrome "109" onward supported; Firefox "161: Partial support"; Safari "26.2" onward "disabled by default".
  Status: VERIFIED (caniuse table as of the research date).

Conclusion for question 3. Yes, a prerender runs the page's JavaScript and would fire any consumption request made on load; so would a Safari top-hit preload if the page consumed on load. Reliable prevention, in order: (1) on the server, answer 503 to any request for the message page that carries Sec-Purpose (prefetch or prefetch;prerender), which cancels the speculation; (2) in the client, never consume on load, only after a user gesture on a reveal control, and additionally wait for document.prerendering to be false (listen for prerenderingchange); (3) make consumption a POST with a client-generated token so that neither a GET nor a cached document can consume. Since cross-site prerender is impossible, the address bar case is the one left after (1), and (2) covers it.

## 4. Security headers and client integrity

- Strict CSP.
  URL: https://web.dev/articles/strict-csp
  Quote: "A Content Security Policy based on nonces or hashes is often called a strict CSP."; example "script-src 'nonce-{RANDOM}' 'strict-dynamic'; object-src 'none'; base-uri 'none';"
  Status: VERIFIED.

- Trusted Types (require-trusted-types-for 'script').
  URL: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/require-trusted-types-for
  Quote: "Disallows using strings with DOM XSS injection sink functions, and requires matching types created by Trusted Type policies."; "Baseline 2026 Newly available. Since February 2026"
  Status: VERIFIED.

- Subresource Integrity covers scripts and stylesheets/preloads, not the HTML document itself.
  URL: https://developer.mozilla.org/en-US/docs/Web/Security/Subresource_Integrity
  Quote: "It works by allowing you to provide a cryptographic hash that a fetched resource must match."; applies to "<script> elements" and "<link> elements whose rel attribute value is stylesheet, preload, or modulepreload."
  Status: VERIFIED (scope); the exclusion of the document is implied by the scope list, not stated.

- Cross-Origin-Opener-Policy.
  URL: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cross-Origin-Opener-Policy
  Quote: "any references between the new document and its opener are severed, and the new document may be process-isolated from its opener."
  Status: VERIFIED.

- Cache-Control: no-store.
  URL: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cache-Control
  Quote: "The no-store response directive indicates that any caches of any kind (private or shared) should not store this response."
  Status: VERIFIED. bfcache: web.dev says browsers "have chosen not to store the page in bfcache" for no-store pages and that there is "work underway to change this behavior for Chrome" (https://web.dev/articles/bfcache). PARTIAL on current Chrome behaviour.

- Clear-Site-Data.
  URL: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Clear-Site-Data
  Quote: "sends a signal to the client that it should remove all browsing data of certain types (cookies, storage, cache)"; "It's been available across browsers since September 2023."
  Status: VERIFIED. It does not clear browser history entries.

- X-Robots-Tag / noindex.
  URL: https://developers.google.com/search/docs/crawling-indexing/robots-meta-tag
  Quote: "The X-Robots-Tag can be used as an element of the HTTP header response for a given URL."; noindex: "Do not show this page, media, or resource in search results."
  Status: VERIFIED. The crawler must fetch the page to see the header; it is not a fetch blocker.

- Isolated Web Apps: signed Web Bundles, not served from the network; enterprise ChromeOS only.
  URL: https://chromeos.dev/en/web/isolated-web-apps and https://github.com/WICG/isolated-web-apps/blob/main/README.md
  Quote: "can't be served from live servers or fetched over the network"; "will only be available to Chrome Enterprise administered ChromeOS devices and select development partners"; explainer: "This document proposes a way of building applications"
  Status: VERIFIED. Status: shipped for enterprise-managed ChromeOS with a developer-mode flag in Chrome/ChromeOS 120+; not usable for a public website.

- WAICT (Web Application Integrity, Consistency, and Transparency): early W3C-backed proposal; Firefox Nightly prototype behind a pref; nothing shipped.
  URL: https://blog.cloudflare.com/improving-the-trustworthiness-of-javascript-on-the-web/ (Oct 16, 2025) and https://hacks.mozilla.org/2026/05/trustworthy-javascript-for-the-open-web/ (May 5, 2026)
  Quote: "WAICT is a W3C-backed effort among browser vendors, cloud providers, and encrypted communication developers"; "We are still very early in the standardization process."; Mozilla: "available behind a pref in Firefox Nightly to help validate the approach"
  Status: VERIFIED (status as of May 2026). Mechanism per Mozilla: websites "cryptographically bind their client-side code to a manifest" that is publicly logged.

- Meta Code Verify: extension that checks a site's published hash manifest; Meta properties.
  URL: https://github.com/facebookincubator/meta-code-verify
  Quote: "An extension for verifying the integrity of a web page"; confirms "Facebook, Messenger, Instagram, and WhatsApp Web code hasn't been tampered with."
  Status: VERIFIED. Browsers: Chrome, Firefox, Edge, Safari. The README does not say it is restricted to Meta sites, but it lists only Meta sites, and the manifest publishing side is Meta's; PARTIAL on reuse by third parties.

- Signed Exchanges: certificate issuance is ending.
  URL: https://developers.google.com/public-key-infrastructure/updates/july2026-sxg-deprecation
  Quote: "Google Trust Services will stop issuing Signed HTTP Exchange (SXG) certificates" ... "within a day or two of September 30th, 2026"; "The Signed HTTP Exchange standard was not widely adopted and it is not being evolved"
  Status: VERIFIED. SXG never gave end users a way to verify application code; it signs a response for cache delivery.

Conclusion for question 4. The headers are reliable for what they do (script injection resistance, referrer suppression, no caching, isolation). No mechanism shipped in a mainstream browser in 2026 lets an ordinary visitor verify that the served web client matches a published version: WAICT is a prototype behind a Nightly pref, IWAs are enterprise ChromeOS only, Code Verify is a Meta-run extension, and SXG is being wound down. Honest wording for the operator: "we publish the source and build hashes; the browser cannot check them for you yet."

## 5. Screenshot and screen-recording detection

- Screen Capture API lets the page capture, and says nothing about detecting or blocking capture of the page.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Screen_Capture_API
  Quote: "let the user select a screen or portion of a screen (such as a window) to capture as a media stream."
  Status: VERIFIED (absence of any detection/blocking feature on the page).

- Encrypted Media Extensions apply to media element playback only.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Encrypted_Media_Extensions_API
  Quote: "provides interfaces for controlling the playback of content which is subject to a digital restrictions management scheme."
  Status: VERIFIED.

- Android FLAG_SECURE is a native window flag.
  URL: https://developer.android.com/security/fraud-prevention/activities
  Quote: "FLAG_SECURE is a Window flag that tells Android not to allow screenshots or to display the window view on a non-secure display"; "When the screenshot is taken the resulting image is blank."
  Status: VERIFIED. (The WindowManager.LayoutParams reference page is JavaScript-rendered and returned only navigation text in four chunks.)

- iOS screenshot notification is native and fires after the fact.
  URL: https://developer.apple.com/documentation/uikit/uiapplication/userdidtakescreenshotnotification (read through https://developer.apple.com/tutorials/data/documentation/uikit/uiapplication/userdidtakescreenshotnotification.json)
  Quote: "A notification that posts when a person takes a screenshot on the device."; "This notification posts after the screenshot is taken."
  Status: VERIFIED.

- Web proposals exist, none adopted: a manifest field to block screenshots in PWAs, and an EME option for capture protection.
  URL: https://lists.w3.org/Archives/Public/public-webapps-github/2024Dec/0645.html (w3c/manifest issue 1154) and https://lists.w3.org/Archives/Public/public-html-media/2024Dec/0000.html (w3c/encrypted-media issue 564)
  Quote: "Prevent Screenshots in PWAs (Issue #1154)" ... "Add a new attribute in the Web Manifest"; EME: "This feature would be best-effort, not guaranteed, depending on OS and DRM system capabilities"
  Status: VERIFIED as open issues from December 2024; no spec text, no browser implementation found.

- Microsoft Edge for Business Screen Capture protection: enterprise-only, policy-driven, in preview.
  URL: https://learn.microsoft.com/en-us/deployedge/microsoft-edge-management-protected-clipboard
  Quote: "restricts screenshots and recordings during protected browsing sessions"; "The Screen Capture protection policy only applies to sites/pages with a Copy:Block Purview DLP policy. It doesn't block screenshots globally"; "Protected Clipboard and related policy controls are currently in Preview."
  Status: VERIFIED. A website cannot request this; the recipient's organisation configures it.

- Chrome or WebKit "protected content" feature for the open web.
  Status: NOT FOUND. Tried: web search for Chrome/WebKit 2025-2026 screenshot prevention or protected content APIs; results were the two W3C issues above, a Mozilla Connect feature request, and Edge enterprise coverage.

Conclusion for question 5: not achievable. A web page can neither detect nor block OS-level screenshots or screen recording. Page-visibility or focus events are not screenshot signals.

## 6. Malicious extensions and malware on the recipient device

- Content scripts run in an isolated world, can read and change the DOM, and are governed by the extension's CSP, not the page's.
  URL: https://developer.chrome.com/docs/extensions/develop/concepts/content-scripts
  Quote: "Content scripts live in an isolated world."; "they are able to read details of the web pages the browser visits, make changes to them"; "Content scripts running in isolated worlds have the following Content Security Policy (CSP):"; "When a content script is injected into the main world, the CSP of the page applies."
  Status: VERIFIED.

- Same from the WebExtensions side.
  URL: https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Content_scripts
  Quote: "Content scripts can access and modify the page's DOM, just like normal page scripts can."
  Status: VERIFIED.

- Host permissions also allow screenshots of the tab and script injection.
  URL: https://developer.chrome.com/docs/extensions/reference/api/tabs
  Quote: "interact directly with the matching tabs using methods such as" captureVisibleTab and scripting.executeScript.
  Status: VERIFIED.

Conclusion for question 6: not achievable. An isolated-world content script sees the decrypted DOM and is invisible to page scripts by design; the page's CSP does not apply to it. Malware with OS access sits below the browser entirely. Any "extension detection" is a weak heuristic (probing for injected DOM nodes or known resource URLs) and should not be presented to users as protection.

## 7. Automation detection

- WebDriver: navigator.webdriver reflects a flag set under remote control.
  URL: https://w3c.github.io/webdriver/#interface
  Quote: "The webdriver-active flag is set to true when the user agent is under remote control. It is initially false."; the attribute "Returns true if webdriver-active flag is set, false otherwise."
  Status: VERIFIED.

- How browsers set it.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Navigator/webdriver
  Quote: "indicates whether the user agent is controlled by automation."; Chrome sets it when "The --enable-automation or --headless flag is used"
  Status: VERIFIED.

- Stealth tooling overrides it.
  URL: https://github.com/berstend/puppeteer-extra/tree/master/packages/puppeteer-extra-plugin-stealth
  Quote: "Stealth mode: Applies various techniques to make detection of headless puppeteer harder."; "navigator.webdriver now returns undefined instead of false"
  Status: VERIFIED (third-party tool README; shows the signal is not trustworthy).

- New headless Chrome is the same browser as headful Chrome.
  URL: https://developer.chrome.com/docs/chromium/new-headless
  Quote: "Chrome now has unified Headless and headful modes."
  Status: VERIFIED. The "HeadlessChrome" UA token: PARTIAL, only third-party guides describe it; no Chrome doc opened mentions it, and the UA is trivially overridden.

- Cloudflare Turnstile: usable without Cloudflare proxying, free tier with a widget cap, privacy statement.
  URL: https://developers.cloudflare.com/turnstile/ and https://developers.cloudflare.com/turnstile/plans/
  Quote: "Turnstile can be embedded into any website without sending traffic through Cloudflare."; "Free users are limited to 20 widgets per account."; "Turnstile does not access, store, or transmit user communications, form entries, or other page inputs."
  Status: VERIFIED. It still loads Cloudflare JavaScript in the recipient's page and sends browser signals to Cloudflare; weigh that against a privacy promise.

- Private Access Tokens / Privacy Pass.
  URL: https://developer.apple.com/news/?id=huqjyh7k, https://developers.cloudflare.com/privacy-pass/, https://www.rfc-editor.org/rfc/rfc9576.html
  Quote: Apple: "prove when HTTP requests are coming from legitimate devices without disclosing someone's identity."; "iOS 16 and macOS Ventura support token type 2"; Cloudflare: "Private Access Tokens, to automatically reduce CAPTCHAs when using iOS 16+ devices on participating websites."; RFC 9576 "The Privacy Pass Architecture"
  Status: VERIFIED. Apple platforms only in practice; requires an issuer relationship (Cloudflare or Fastly).

Conclusion for question 7: heuristic only. navigator.webdriver is honest for cooperative automation and false-negative under stealth tooling; UA tokens are spoofable; Turnstile and PATs raise the cost of bulk automation but cannot prove a human. None of it should gate the one-time consumption on its own; a user gesture plus POST is the primary control.

## 8. Web Crypto availability

- Web Crypto API and SubtleCrypto are Baseline widely available and require a secure context; AES-GCM, HKDF and PBKDF2 are documented parameters.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Web_Crypto_API and https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto
  Quote: "It's been available across browsers since July 2015." (Web Crypto) and "since September 2017" (SubtleCrypto); "Secure context: This feature is available only in secure contexts (HTTPS), in some or all supporting browsers."; algorithm rows "AES-GCM", "HKDF", "PBKDF2" listed under encrypt/deriveKey.
  Status: VERIFIED.

- Safari and iOS Safari support.
  URL: https://caniuse.com/cryptography
  Quote: Safari "7.1 - 10.1: Supported"; Safari on iOS "8 - 10.3: Supported" (and all later versions).
  Status: VERIFIED. Practical note: crypto.subtle is undefined on plain http:// origins other than localhost.

## 9. Downloads and memory on mobile

- Blob plus the download attribute on iOS Safari.
  URL: https://caniuse.com/download
  Quote: iOS Safari "3.2 - 12.5: Not supported" and "13 - 26.6: Supported".
  Status: VERIFIED (caniuse). No notes on blob: URLs or size limits appear on that page.

- WebAssembly memory limits in iOS Safari.
  URL: https://bugs.webkit.org/show_bug.cgi?id=269777
  Quote: "Only ~300 MB of RAM is reliably available to WebAssembly pages"; "the page is able to allocate about ~1GB of RAM before being exhausted"; resolved as duplicate of 269937 "MegaBug: WASM memory, SAB and Gigacage fragmentation, causes unpredictable wasm memory allocation".
  Status: NOT FOUND for an official documented limit; PARTIAL evidence from WebKit's own bug tracker that the practical ceiling is unpredictable and often well under 1 GB. Tried: web search for Apple/WebKit documentation of a fixed cap; none surfaced.

## 10. Clipboard API and Web Share API

- Clipboard writeText: secure context, and a user gesture (or clipboard-write permission in Chromium).
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Clipboard/writeText and https://developer.mozilla.org/en-US/docs/Web/API/Clipboard_API
  Quote: "Writing to the clipboard can only be done in a secure context."; "Writing requires either the clipboard-write permission or transient activation." (Chromium); "Writing requires transient activation." (Firefox and Safari); available "since March 2020".
  Status: VERIFIED.

- Web Share: secure context, transient activation, not Baseline; no Firefox desktop.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/Navigator/share and https://caniuse.com/web-share
  Quote: "requires that the current document have the web-share Permissions Policy and transient activation."; "This feature is not Baseline because it does not work in some of the most widely-used browsers."; caniuse: Firefox desktop not supported in any listed version; Safari macOS from 12.1, iOS Safari from 12.2, Chrome desktop fully from 128.
  Status: VERIFIED.

## 11. history.replaceState and the fragment

- replaceState modifies the current session history entry; it does not add one.
  URL: https://developer.mozilla.org/en-US/docs/Web/API/History/replaceState and https://developer.mozilla.org/en-US/docs/Web/API/History_API/Working_with_the_History_API
  Quote: "modifies the current history entry, replacing it with the state object and URL passed in the method parameters."; "the replaceState() method updates the session history entry for the current page."
  Status: VERIFIED.

- The HTML spec's URL and history update steps replace the active entry.
  URL: https://html.spec.whatwg.org/multipage/browsing-the-web.html#url-and-history-update-steps
  Quote: "Let entryToReplace be activeEntry if historyHandling is "replace", otherwise null."; "Set navigable's active session history entry to newEntry."
  Status: VERIFIED. After replaceState the session history (back/forward) no longer holds the URL with the fragment.

- Global browsing history (chrome://history, and therefore Chrome Sync) is a separate store. The first load commits with the full URL before any script runs, and Chromium also records same-document navigations.
  URL: https://chromium.googlesource.com/chromium/src/+/main/chrome/browser/history/history_tab_helper.cc
  Quote: "navigation_handle->IsSameDocument() || navigation_handle->GetReloadType() != content::ReloadType::NONE"; "UpdateHistoryForNavigation(add_page_args);"
  Status: PARTIAL. This is a source reading, not documentation: DidFinishNavigation adds committed navigations, including same-document ones, to the history database and shows no special-casing that would remove the earlier visit. The original URL with the fragment therefore stays in the browser's history store even after replaceState scrubs the session entry. Safari and Firefox global-history behaviour: NOT FOUND in docs; a web search returned nothing authoritative. Treat the key as present in local history on all browsers until measured.

## Decision-relevant conclusions

- The fragment never reaches your server or any Referer header: RFC 3986, RFC 9110 and the Referrer Policy spec all say so. Reliable.
- The fragment does land in the recipient's browser history and, when sync is on, in vendor cloud history: Chrome only with a passphrase is it unreadable by Google; Firefox Sync is end-to-end by default. replaceState cleans the session entry but not the global history store. Reliable for the spec statements; heuristic (source reading) for Chrome's history database. Design for the key being persisted locally; keep messages one-time and short-lived rather than relying on URL scrubbing.
- Any script in the page, any extension with "tabs" or host permissions, and any content script can read the fragment or the decrypted DOM. Not achievable to prevent or detect.
- Unfurlers fetch with GET from vendor servers within seconds of posting (Slack, Meta, WhatsApp, Teams documented). A GET must never consume. Reliable.
- Whether unfurlers run JavaScript is undocumented except Apple Messages (no) and Googlebot (yes); Safe Links scans at delivery and click and detonates unknown URLs in a sandbox. POST-only consumption defeats HTML-only fetchers reliably; against JS-executing scanners it is a heuristic, so require a user click, a client-generated token, and refuse requests carrying Sec-Purpose or known crawler UAs.
- Prerender runs JavaScript. Cross-site prerender is impossible, so the exposure is the Chrome address bar and your own pages. Return 503 to Sec-Purpose requests, gate consumption on a click, and check document.prerendering. Reliable in Chromium; Safari's top-hit preload is undocumented as to JS, so the click gate is the fallback.
- Strict CSP with nonces and strict-dynamic, Trusted Types (Baseline since February 2026), SRI, COOP, Referrer-Policy: no-referrer, Cache-Control: no-store, Clear-Site-Data and X-Robots-Tag are all reliable for their stated effects. None lets a visitor verify the served code.
- Client code verification by the browser: not achievable for a public site in 2026. WAICT is a Nightly prototype behind a pref, IWAs are enterprise ChromeOS, Code Verify is Meta's own, and SXG certificates stop in September 2026. Say so plainly in the threat model.
- Screenshot or screen-recording detection or blocking from a web page: not achievable. FLAG_SECURE and the iOS notification are native only; the W3C proposals are open issues; Edge's protection is an enterprise DLP policy.
- Automation detection: heuristic only. navigator.webdriver is honest for cooperating tools and overridden by stealth plugins; UA strings are spoofable; Turnstile and Private Access Tokens raise cost, not certainty, and Turnstile adds a third-party script.
- Web Crypto with AES-GCM, HKDF and PBKDF2 is Baseline and works on Safari and iOS since 2015 and iOS 8; it needs HTTPS. Reliable.
- Blob downloads via the download attribute work on iOS Safari 13 and later. Reliable (caniuse). WebAssembly memory on iOS is unpredictable and often under 1 GB per WebKit's own bug tracker; keep client-side decryption streaming or small.
- Clipboard writes and Web Share need a secure context and a user gesture; Web Share is absent on Firefox desktop, so keep a copy-to-clipboard fallback. Reliable.
- X-Robots-Tag: noindex and robots.txt do not stop fetches by user-triggered fetchers (Google documents that they ignore robots.txt). Treat every link as fetched by at least one bot.

## URLs opened

- https://www.rfc-editor.org/rfc/rfc3986#section-3.5
- https://www.rfc-editor.org/rfc/rfc9110.html (sections 7.1 and 10.1.3)
- https://w3c.github.io/webappsec-referrer-policy/
- https://fetch.spec.whatwg.org/
- https://html.spec.whatwg.org/multipage/browsing-the-web.html
- https://developer.mozilla.org/en-US/docs/Web/API/Location/hash
- https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Referrer-Policy
- https://developer.chrome.com/docs/extensions/develop/concepts/declare-permissions
- https://developer.chrome.com/docs/extensions/reference/api/tabs
- https://support.google.com/chrome/answer/1181035
- https://support.google.com/chrome/answer/185277
- https://support.google.com/chrome/answer/165139
- https://support.google.com/chrome/answer/1385029
- https://support.google.com/chrome/answer/114836
- https://www.google.com/chrome/privacy/ (notice withdrawn)
- https://www.google.com/chrome/privacy/whitepaper.html (no text rendered)
- https://support.mozilla.org/en-US/kb/how-firefox-securely-saves-passwords (no text rendered)
- https://support.mozilla.org/en-US/kb/how-firefox-sync-keeps-your-data-safe-even-if-tls-fails (no text rendered)
- https://hacks.mozilla.org/2018/11/firefox-sync-privacy/
- https://api.slack.com/robots
- https://docs.slack.dev/messaging/unfurling-links-in-messages/
- https://developers.facebook.com/docs/sharing/webmasters/web-crawlers/
- https://developers.facebook.com/documentation/business-messaging/whatsapp/link-previews/
- https://bugs.telegram.org/c/65828
- https://support.discord.com/hc/en-us/articles/42500550752919-About-Discord-Link-Previews-and-the-Discordbot (HTTP 403)
- https://developer.apple.com/tutorials/data/documentation/technotes/tn3156-create-rich-previews-for-messages.json
- https://support.microsoft.com/en-us/office/how-to-update-your-connected-service-settings-893f468a-4e7e-4f33-9e53-bda8ad0f8fcc
- https://developers.google.com/workspace/add-ons/chat/preview-links
- https://developers.google.com/crawling/docs/crawlers-fetchers/google-user-triggered-fetchers
- https://developers.google.com/search/docs/crawling-indexing/javascript/javascript-seo-basics
- https://learn.microsoft.com/en-us/defender-office-365/safe-links-about
- https://support.google.com/mail/answer/10173182?hl=en
- https://developer.chrome.com/docs/web-platform/prerender-pages
- https://developer.mozilla.org/en-US/docs/Web/API/Speculation_Rules_API
- https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Sec-Purpose
- https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Supports-Loading-Mode
- https://developer.mozilla.org/en-US/docs/Web/API/Document/prerendering
- https://developer.mozilla.org/en-US/docs/Web/Performance/Guides/Speculative_loading
- https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Attributes/rel/prefetch
- https://support.apple.com/guide/safari/sfria1042d31/mac
- https://caniuse.com/mdn-html_elements_script_type_speculationrules
- https://web.dev/articles/strict-csp
- https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/require-trusted-types-for
- https://developer.mozilla.org/en-US/docs/Web/Security/Subresource_Integrity
- https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cross-Origin-Opener-Policy
- https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cache-Control
- https://web.dev/articles/bfcache
- https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Clear-Site-Data
- https://developers.google.com/search/docs/crawling-indexing/robots-meta-tag
- https://chromeos.dev/en/web/isolated-web-apps
- https://github.com/WICG/isolated-web-apps/blob/main/README.md
- https://blog.cloudflare.com/improving-the-trustworthiness-of-javascript-on-the-web/
- https://hacks.mozilla.org/2026/05/trustworthy-javascript-for-the-open-web/
- https://github.com/facebookincubator/meta-code-verify
- https://developers.google.com/public-key-infrastructure/updates/july2026-sxg-deprecation
- https://developer.mozilla.org/en-US/docs/Web/API/Screen_Capture_API
- https://developer.mozilla.org/en-US/docs/Web/API/Encrypted_Media_Extensions_API
- https://developer.android.com/reference/android/view/WindowManager.LayoutParams (no reference text rendered)
- https://developer.android.com/security/fraud-prevention/activities
- https://developer.apple.com/tutorials/data/documentation/uikit/uiapplication/userdidtakescreenshotnotification.json
- https://lists.w3.org/Archives/Public/public-webapps-github/2024Dec/0645.html
- https://lists.w3.org/Archives/Public/public-html-media/2024Dec/0000.html
- https://learn.microsoft.com/en-us/deployedge/microsoft-edge-management-protected-clipboard
- https://developer.chrome.com/docs/extensions/develop/concepts/content-scripts
- https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Content_scripts
- https://w3c.github.io/webdriver/#interface
- https://developer.mozilla.org/en-US/docs/Web/API/Navigator/webdriver
- https://developer.chrome.com/docs/chromium/headless
- https://developer.chrome.com/docs/chromium/new-headless
- https://github.com/berstend/puppeteer-extra/tree/master/packages/puppeteer-extra-plugin-stealth
- https://developers.cloudflare.com/turnstile/
- https://developers.cloudflare.com/turnstile/plans/
- https://developers.cloudflare.com/privacy-pass/
- https://developer.apple.com/news/?id=huqjyh7k
- https://www.rfc-editor.org/rfc/rfc9576.html
- https://developer.mozilla.org/en-US/docs/Web/API/Web_Crypto_API
- https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto
- https://caniuse.com/cryptography
- https://caniuse.com/download
- https://bugs.webkit.org/show_bug.cgi?id=269777
- https://developer.mozilla.org/en-US/docs/Web/API/Clipboard/writeText
- https://developer.mozilla.org/en-US/docs/Web/API/Clipboard_API
- https://developer.mozilla.org/en-US/docs/Web/API/Navigator/share
- https://caniuse.com/web-share
- https://developer.mozilla.org/en-US/docs/Web/API/History/replaceState
- https://developer.mozilla.org/en-US/docs/Web/API/History_API/Working_with_the_History_API
- https://chromium.googlesource.com/chromium/src/+/main/chrome/browser/history/history_tab_helper.cc
