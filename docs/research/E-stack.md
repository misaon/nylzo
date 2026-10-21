# E. Stack research: versions, runtime capabilities, distribution and supply chain

Research date: 2026-10-21. Every claim below lists the URL opened, a short quote copied from that page, and a status (VERIFIED, PARTIAL, NOT FOUND). npm pages (npmjs.com) returned HTTP 403 to the fetcher, so package versions and publish dates come from the npm registry API (`https://registry.npmjs.org/<package>`, fields `dist-tags.latest` and `time`). crates.io pages render client side, so Rust crate versions come from `https://crates.io/api/v1/crates/<crate>`.

## 1. Versions and support windows

### Node.js

- Claim: Node.js 26 is Current (v26.12.1), 24 and 22 are LTS, 25 and 20 are EOL.
  URL: https://nodejs.org/en/about/previous-releases
  Quote: table rows "v26 ... May 05, 2026 ... Current", "v24 Krypton ... LTS", "v22 Jod ... LTS", "v25 ... Mar 31, 2026 ... EOL", "v20 Iron ... Mar 24, 2026 ... EOL"
  Status: VERIFIED. Latest versions shown: v26.12.1, v24.22.1, v22.23.4.
- Claim: LTS windows. 22.x: Maintenance LTS since 2025-10-21, EOL 2027-04-30. 24.x: Active LTS from 2025-10-28, Maintenance from 2026-10-20, EOL 2028-04-30. 26.x: Current, Active LTS from 2026-10-28, Maintenance 2027-10-20, EOL 2029-04-30.
  URL: https://raw.githubusercontent.com/nodejs/Release/main/README.md
  Quote: "Every even (LTS) major version is actively maintained for 12 months from the date it enters LTS coverage."
  Status: VERIFIED. The README table still labels 24.x "Active LTS" although its Maintenance start date (2026-10-20) was yesterday; 26.x enters Active LTS next week (2026-10-28).

### TypeScript and the native compiler

- Claim: TypeScript latest stable on npm is 7.0.2, published 2026-07-08; `beta` tag 7.1.0-beta, `next` 7.1.0-dev.20261021.1.
  URL: https://registry.npmjs.org/typescript
  Quote: `"latest": "7.0.2"`, `"time": {"7.0.2": "2026-07-08T15:55:18.431Z"}`
  Status: VERIFIED.
- Claim: GitHub marks v7.0.2 as the latest release and 7.1 Beta as a pre-release.
  URL: https://github.com/microsoft/TypeScript/releases
  Quote: "TypeScript 7.0.2" marked "Latest"; "TypeScript 7.1 Beta" (20 Oct) marked "Pre-release"
  Status: VERIFIED. The GitHub release entry shows "20 Aug 18:09" while the registry publish date is 2026-07-08; both are reported as shown.
- Claim: TypeScript 7.0 is the Go-based native compiler; announced 2026-06-18. TypeScript 6.0 was announced 2026-03-06.
  URL: https://devblogs.microsoft.com/typescript/
  Quote: "The mission was a native port of TypeScript built in Go that could make the most of modern hardware." ("Announcing TypeScript 7.0", Jun 18, 2026)
  Status: VERIFIED.
- Claim: The typescript-go staging repo is closed; the port shipped as TS 7.0 but the LSP is still in progress and the API is not ready.
  URL: https://github.com/microsoft/typescript-go
  Quote: "This was the staging repo for the TypeScript 7.0 release during the native port process, which is now completed!"
  Status: VERIFIED. README feature table: Language service (LSP) "in progress", API "not ready"; "For TypeScript 7.0 RC and later, the command name is tsc". The preview package `@typescript/native-preview` last published 2026-07-07 (7.0.0-dev.20260707.2).

### Frontend and tooling

- Claim: Vite 8.3.5, published 2026-10-21.
  URL: https://registry.npmjs.org/vite
  Quote: `"latest": "8.3.5"`, time "2026-10-21T06:38:54.907Z"
  Status: VERIFIED.
- Claim: Vue 3.5.44, published 2026-10-13; Vue 3.6 is at `rc` (3.6.0-rc.12).
  URL: https://registry.npmjs.org/vue
  Quote: `"latest": "3.5.44"`, `"rc": "3.6.0-rc.12"`
  Status: VERIFIED.
- Claim: Svelte 5.57.2 (2026-10-06); SvelteKit 3.1.0 (2026-10-14).
  URL: https://registry.npmjs.org/svelte and https://registry.npmjs.org/@sveltejs/kit
  Quote: `"latest": "5.57.2"`; `"latest": "3.1.0"`
  Status: VERIFIED.
- Claim: Hono 4.13.13 (2026-10-04); Hono v5 is at release candidate (`next` tag 5.0.0-rc.1).
  URL: https://registry.npmjs.org/hono and https://github.com/honojs/hono/releases
  Quote: `"latest": "4.13.13"`, `"next": "5.0.0-rc.1"`; GitHub: "v5.0.0-rc.1" pre-release "released this 10 Oct 12:34"
  Status: VERIFIED. The rc.1 release page has no release notes body (https://github.com/honojs/hono/releases/tag/v5.0.0-rc.1). `@hono/node-server` is 2.1.5 (2026-10-13).
- Claim: Wrangler 4.151.3 (2026-10-20); workerd 1.20261021.1 (2026-10-21).
  URL: https://registry.npmjs.org/wrangler, https://registry.npmjs.org/workerd, https://github.com/cloudflare/workers-sdk/releases
  Quote: "wrangler@4.151.3" "released this 20 Oct 12:14"
  Status: VERIFIED. Also: `@cloudflare/workers-types` 5.20261021.1, `@cloudflare/vite-plugin` 1.64.0 (2026-10-14), miniflare 4.20260730.0.
- Claim: Bun 1.4.3 (2026-10-10).
  URL: https://github.com/oven-sh/bun/releases
  Quote: "Bun v1.4.3" marked "Latest", "released this 10 Oct 05:24"
  Status: VERIFIED (year from asset timestamps 2026-10-10).
- Claim: Deno 2.9.7 (2026-09-16).
  URL: https://github.com/denoland/deno/releases
  Quote: "v2.9.7" "Latest"; changelog heading "2.9.7 / 2026.09.16"
  Status: VERIFIED.
- Claim: pnpm 12.14.0 (2026-10-15). pnpm 11.0.0 shipped 2026-04-28, 12.0.0 on 2026-08-26.
  URL: https://registry.npmjs.org/pnpm
  Quote: `"latest": "12.14.0"`, `"latest-11": "11.28.6"`, time "11.0.0": "2026-04-28T09:34:08.502Z"
  Status: VERIFIED.
- Claim: Vitest 5.0.5 (2026-10-15); Playwright 1.64.1 (2026-10-14).
  URL: https://registry.npmjs.org/vitest and https://registry.npmjs.org/playwright
  Quote: `"latest": "5.0.5"`; `"latest": "1.64.1"`
  Status: VERIFIED.

### Go and Rust

- Claim: Go latest stable is go1.27.3, released 2026-10-19 (Go 1.27.0 on 2026-08-19).
  URL: https://go.dev/doc/devel/release (and https://go.dev/dl/)
  Quote: "go1.27.3 (released 2026-10-19) includes security fixes to the crypto/x509 package."
  Status: VERIFIED.
- Claim: Rust latest stable is 1.99.1, announced 2026-10-15.
  URL: https://blog.rust-lang.org/ and https://github.com/rust-lang/rust/releases
  Quote: "Announcing Rust 1.99.1" dated "Oct. 15" (2026); GitHub "Rust 1.99.1" "Latest"
  Status: VERIFIED.

### Container base images

- Claim: Docker official `node` image has `24-alpine`, `lts-alpine`, `26`, `26-alpine`, `latest` (=26, Debian trixie); no node:25 tags remain. 24 images are Debian bookworm based; 26 images are trixie based.
  URL: https://hub.docker.com/_/node
  Quote: "24-alpine, 24-alpine3.24, 24.22-alpine ... lts-alpine"; "26, 26-trixie, 26.12, ... current, current-trixie, latest, trixie"
  Status: VERIFIED. Current images: node 24.22.1, 26.12.1, 22.23.4; alpine 3.24.
- Claim: Chainguard publishes `cgr.dev/chainguard/node`; only `latest` and `latest-dev` are pullable on the free tier, version-pinned tags (22/24/25/26) need a contract.
  URL: https://images.chainguard.dev/directory/image/node/versions and https://images.chainguard.dev/directory/image/node/overview
  Quote: "Try a starter image for free"; versioned tags show "Contact us for access to this image."; "These minimal container images typically do not contain a shell or package manager."
  Status: PARTIAL. The page does not state the free-tier rule in one sentence; it is inferred from which tags show a pull URL.
- Claim: Google distroless ships `gcr.io/distroless/nodejs22-debian13`, `nodejs24-debian13`, `nodejs26-debian13` with tags latest, nonroot, debug, debug-nonroot.
  URL: https://github.com/GoogleContainerTools/distroless
  Quote: "gcr.io/distroless/nodejs24-debian13", "gcr.io/distroless/nodejs26-debian13"
  Status: VERIFIED.

## 2. Hono

- Claim: Official supported runtimes.
  URL: https://hono.dev/docs/
  Quote: "Works on Cloudflare Workers, Fastly Compute, Deno, Bun, AWS Lambda, or Node.js. The same code runs on all platforms."
  Status: VERIFIED.
- Claim: Version 4.13.13 (see section 1).
- Claim: Hono has a steady stream of GHSA advisories in 2026 (page 1 of 6 on the repo's advisory list, all Moderate or Low). IDs seen on the page:
  URL: https://github.com/honojs/hono/security/advisories
  - GHSA-5r4p-p66f-jhc7 "serveStatic decodes the request path a second time, leading to bypass of middleware on static paths" (Moderate, Sep 29, 2026)
  - GHSA-hxh3-vqpv-xpqv "hono/jsx renders plain strings unescaped in boundary components, leading to XSS" (Moderate, Sep 4, 2026)
  - GHSA-g6gw-c38x-mqfc "Unbounded dot-notation nesting in parseBody() can cause memory exhaustion" (Moderate, Aug 26, 2026)
  - GHSA-gqvv-2mrq-wpjv "Incomplete fix for CVE-2026-39408: toSSG() still writes files outside the output directory" (Moderate, Aug 26, 2026)
  - GHSA-crvj-82cr-hjcx "Query parser reads parameters after the URL fragment, causing cache-key and proxy interpretation differentials" (Moderate, Aug 26, 2026)
  - GHSA-f23p-vx2j-j53r "memo() retains SSR output across requests, leading to cross-user data disclosure" (Moderate, Aug 3, 2026)
  - GHSA-79qm-7rj5-m7r9 "Proxy Helper does not remove response headers listed in the Connection header" (Low, Aug 3, 2026)
  - GHSA-54fx-42gc-7vw4 "Algorithmic Complexity DoS in Language Middleware" (Moderate, Aug 3, 2026)
  - GHSA-8j4g-w8fx-2239 "ReDoS in CORS middleware via Access-Control-Request-Headers" (Moderate, Aug 3, 2026)
  - GHSA-xgm2-5f3f-mvvc "API Gateway v1 adapter can drop a distinct repeated request header value during de-duplication" (Moderate, Jun 23, 2026)
  Status: PARTIAL. Only page 1 of 6 was opened; 2025 advisories sit on later pages. The GitHub Advisory Database search (https://github.com/advisories?query=hono) additionally shows GHSA-hvrm-45r6-mjfj "hono/jsx does not isolate context per request" and GHSA-w62v-xxxg-mg59 "Server-Side XSS via JSX Escaping Bypass in cx() Utility" (both Jul 21, 2026).
- Claim: Details of the two most relevant advisories.
  URL: https://github.com/advisories/GHSA-5r4p-p66f-jhc7
  Quote: affected "hono >= 4.12.4, < 4.13.11", patched 4.13.11, CVE-2026-111757, CVSS 5.3; "serveStatic decodes the request path a second time after Hono has already decoded it for routing."
  Status: VERIFIED. (GHSA-3g9q-qwv8-cxpf is an unreviewed duplicate, CVE-2026-109955.)
  URL: https://github.com/advisories/GHSA-f23p-vx2j-j53r
  Quote: affected ">= 3.8.0, < 4.12.34", patched 4.12.34, CVE-2026-71850, CVSS 4.8
  Status: VERIFIED. Most 2026 issues are in hono/jsx, SSG, proxy and language middleware, which a JSON API does not use; serveStatic, CORS and parseBody are the ones that matter for this app.

## 3. Cloudflare Workers runtime

- Claim: Node.js compatibility is now on by default for new compatibility dates.
  URL: https://developers.cloudflare.com/workers/runtime-apis/nodejs/
  Quote: "For compatibility dates of 2026-08-04 or later, Workers enables both nodejs_compat and nodejs_compat_v2 by default."
  Status: VERIFIED. Natively supported modules listed: Buffer, Crypto, Events, File system, HTTP/HTTPS, Net, Path, Process, Stream, Timers, URL, Utilities, Zlib, Web Crypto, Web Streams. Partial: Console, DNS, Module, OS, Performance hooks, Test runner, TLS. Non-functional stubs: `node:http2`, `node:vm`, `node:child_process`, `node:worker_threads`, `node:sqlite`.
- Claim: Web Crypto on Workers supports RSASSA-PKCS1-v1_5, RSA-PSS, RSA-OAEP, ECDSA, ECDH, Ed25519, X25519, HMAC, AES-CTR/CBC/GCM/KW, SHA-1/256/384/512, HKDF, PBKDF2, plus non-standard MD5, `timingSafeEqual` and `DigestStream`.
  URL: https://developers.cloudflare.com/workers/runtime-apis/web-crypto/
  Quote: "MD5 is not part of the WebCrypto standard but is supported in Cloudflare Workers"; timingSafeEqual: "This is a non-standard extension to the Web Crypto API."
  Status: VERIFIED. The page lists no key-size limits. Argon2/scrypt are not in Web Crypto, so password KDF work must run client side or use PBKDF2/HKDF.
- Claim: Durable Objects SQLite storage API basics.
  URL: https://developers.cloudflare.com/durable-objects/api/sqlite-storage-api/
  Quote: "Multiple SQL statements, separated with a semicolon, can be executed in the query."; transactionSync: "The callback must complete synchronously"
  Status: VERIFIED. `sql.exec(query, ...bindings)` returns a cursor (`next`, `toArray`, `one`, `raw`, `columnNames`, `rowsRead`, `rowsWritten`); `databaseSize`; synchronous KV API on `ctx.storage.kv`; point-in-time recovery bookmarks for 30 days; `get`/`put`/`delete` batches of up to 128 keys.
- Claim: DO SQLite limits: 2 MB max row/string/BLOB, 100 KB SQL statement, 100 bound parameters, 100 columns per table, 10 GB per object (paid), 5 GB total storage on Free, 100 DO classes on Free.
  URL: https://developers.cloudflare.com/durable-objects/platform/limits/
  Quote: "Maximum string, BLOB or table row size" "2 MB"; "Storage per account" "Unlimited (Workers Paid) / 5GB (Free)"; footnote "Accounts on the Workers Free plan are limited to 5 GB total Durable Objects storage."
  Status: VERIFIED. The page is inconsistent on per-object storage on Free: the FAQ says "(10 GB on Workers Paid, or 1 GB on the Free plan)" while the table says 10 GB with the 5 GB account footnote. Plan for 1 GB per object on Free.
- Claim: Free plan DO allowances.
  URL: https://developers.cloudflare.com/durable-objects/platform/pricing/
  Quote: "Workers Free plan can only create and access SQLite-backed Durable Objects."
  Status: VERIFIED. Free: 100,000 requests/day, 13,000 GB-s/day, 5 million rows read/day, 100,000 rows written/day, 5 GB stored.
- Claim: R2 limits: 5 TiB per object; 5 GiB single-part upload; 4.995 TiB multipart; 10,000 parts; part size 5 MiB to 5 GiB, equal sizes except the last.
  URL: https://developers.cloudflare.com/r2/platform/limits/ and https://developers.cloudflare.com/r2/objects/multipart-objects/
  Quote: "5 GiB (single-part) / 4.995 TiB (multi-part)"; "Minimum part size: 5 MiB (except for the last part)"; "All parts except the last must be the same size"
  Status: VERIFIED. Multipart is available via the Workers binding (`createMultipartUpload()`, `resumeMultipartUpload()`) and the S3 API. Object key max 1,024 bytes, metadata 8,192 bytes.
- Claim: R2 presigned URLs support GET, HEAD, PUT, DELETE, expiry up to 7 days, generated with SigV4 offline.
  URL: https://developers.cloudflare.com/r2/api/s3/presigned-urls/
  Quote: "Timeout from 1 second to 7 days (604,800 seconds)"; "POST (multipart form uploads via HTML forms) is not currently supported."
  Status: VERIFIED. Presigned URLs are "generated server-side with no communication with R2", so any SigV4 implementation works (the page shows AWS SDK, boto3, AWS CLI).
- Claim: Workers Free plan limits.
  URL: https://developers.cloudflare.com/workers/platform/limits/
  Quote: "There is no compressed size limit. Only the uncompressed bundle size counts."; "Worker size (uncompressed) | 64 MiB | 64 MiB"; "Requests | 100,000/day"; "CPU time per HTTP request | 10 ms"
  Status: VERIFIED. Also: request body "Maximum request body size" "100 MB" on the Free Cloudflare plan (account plan, not Workers plan); 50 subrequests per invocation; 128 MB memory; 100 Workers per account; 64 variables per Worker, 5 KB each; 1 s startup; 6 simultaneous outgoing connections; Cron Triggers "5" (Free) / "250" (Paid) per account; static assets "Files per Worker version | 20,000 | 100,000" and "Individual file size | 25 MiB".
- Claim: Workers Static Assets can serve a SPA from the same Worker, and asset requests are free.
  URL: https://developers.cloudflare.com/workers/static-assets/ , https://developers.cloudflare.com/workers/static-assets/routing/single-page-application/ , https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/
  Quote: "Requests to static assets are free and unlimited."; "if a requested URL matches a file in the static assets directory, that file will be served — without invoking Worker code."; config `not_found_handling = "single-page-application"` and `run_worker_first = [ "/api/*", "!/api/docs/*" ]`
  Status: VERIFIED. Requests matched by `run_worker_first` count against the Worker request quota and get 429 once the Free tier is exhausted.
- Claim: The rate limiting binding is GA and configured with `simple.limit` and `simple.period` (10 or 60 seconds).
  URL: https://developers.cloudflare.com/workers/runtime-apis/bindings/rate-limit/ and https://developers.cloudflare.com/changelog/post/2025-09-19-ratelimit-workers-ga/
  Quote: "The ratelimit binding is now stable and recommended for all production workloads." (Sept 19, 2025); "Must be either 10 or 60"
  Status: VERIFIED for API and GA; PARTIAL for plan availability (neither page mentions the Free plan). Requires Wrangler 4.36.0+. `limit({ key })` returns `{ success }`.
- Claim: Cron Triggers use `triggers.crons` plus a `scheduled` handler; Free plan allows 5 per account.
  URL: https://developers.cloudflare.com/workers/configuration/cron-triggers/
  Quote: "To respond to a Cron Trigger, you must add a "scheduled" handler to your Worker."
  Status: VERIFIED (limit from the Limits page above; CPU time per Cron Trigger is 10 ms on Free).
- Claim: "Deploy to Cloudflare" button and GitHub integration work for public repos; Workers Builds has a Free tier.
  URL: https://developers.cloudflare.com/workers/platform/deploy-buttons/ , https://developers.cloudflare.com/workers/ci-cd/builds/limits-and-pricing/ , https://developers.cloudflare.com/workers/ci-cd/builds/
  Quote: `https://deploy.workers.cloudflare.com/?url=<your git repo URL>`; "Cloudflare clones your source repository into the user's GitHub/GitLab account."; "Repositories must be public in order for others to successfully use your Deploy to Cloudflare button."; Free plan "3,000 per month" build minutes, "1" concurrent build, "20 minutes" timeout
  Status: VERIFIED. Builds connect to "a repository hosted by Artifacts, GitHub, or GitLab".

## 4. Portability pattern

- Claim: The same Hono app object runs on Workers (`export default app`) and Node (`serve({ fetch: app.fetch })`).
  URL: https://hono.dev/docs/getting-started/cloudflare-workers and https://hono.dev/docs/getting-started/nodejs
  Quote: Workers: `app.get('/', (c) => c.text('Hello Cloudflare Workers!'))` then "export default app"; Node: "serve({ fetch: app.fetch, port: 8787, })"; "It works on Node.js versions greater than 18.x."
  Status: VERIFIED. Workers page notes "By default, process.env is not available in Cloudflare Workers" (read `c.env` instead), and static files go through Workers Static Assets (`"assets": { "directory": "public" }`), while Node uses `serveStatic` from `@hono/node-server/serve-static`.
- Reasoning (storage adapter): Workers has no persistent filesystem. The nodejs_compat page lists "File system" as supported, but that is a virtual in-process filesystem, and `node:sqlite` is explicitly a non-functional stub on Workers. The Node build will use `node:sqlite` or better-sqlite3 on disk; the Workers build must use Durable Objects SQLite (or D1). The API surface shared by both is small (prepare/exec/transaction), so define one storage interface with two implementations and keep SQL within the DO limits (2 MB per row, 100 bound parameters, 100 KB statement). Blob storage follows the same split: R2 on Workers, local disk or any S3-compatible bucket in Docker.
- Claim: `node:sqlite` stability in the LTS lines.
  URL: https://nodejs.org/docs/latest-v22.x/api/sqlite.html
  Quote: "Stability: 1.1 - Active development." (added v22.5.0; "SQLite is no longer behind --experimental-sqlite but still experimental" since v22.13.0)
  Status: VERIFIED.
  URL: https://nodejs.org/docs/latest-v24.x/api/sqlite.html
  Quote: "Stability: 1.2 - Release candidate." (history: "v24.15.0 SQLite is now a release candidate.")
  Status: VERIFIED.
  URL: https://nodejs.org/docs/latest-v26.x/api/sqlite.html
  Quote: "Stability: 1.2 - Release candidate." (v25.7.0 entry; v26 adds `serialize()`/`deserialize()` in 26.1.0, `createModule()` in 26.11.0)
  Status: VERIFIED. No Node line has `node:sqlite` at Stability 2 (Stable) yet, but no flag is needed on 22.13+, 24 and 26.
- Claim: better-sqlite3 13.0.3, published 2026-08-05.
  URL: https://registry.npmjs.org/better-sqlite3
  Quote: `"latest": "13.0.3"`
  Status: VERIFIED. Native addon; needs a prebuilt binary per Node major and per arch in the Docker image.
- Claim: Litestream v0.5.17 (2026-08-31) streams SQLite to object storage; replica guides cover S3, S3-compatible services, Azure Blob, GCS, B2, SFTP, local path, NATS, WebDAV.
  URL: https://github.com/benbjohnson/litestream/releases , https://litestream.io/ , https://litestream.io/guides/
  Quote: "Continuously stream SQLite changes to your preferred cloud storage or local files."; "Supported Platforms: Linux (amd64, arm64, armv6, armv7), macOS (amd64, arm64)"
  Status: VERIFIED. There is no dedicated R2 guide; R2 falls under "Replicating to S3-Compatible Services". The read-only VFS extension is marked "Experimental".

## 5. Distribution of a self-host Docker image

- Claim: ghcr.io is free for public images.
  URL: https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-packages/about-billing-for-github-packages
  Quote: "GitHub Packages usage is free for public packages."
  Status: VERIFIED. Private packages on GitHub Free: 500 MB storage, 1 GB transfer per month.
- Claim: GitHub Actions is free on public repos with standard hosted runners.
  URL: https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-actions/about-billing-for-github-actions
  Quote: "GitHub Actions usage is free for self-hosted runners and for public repositories that use standard GitHub-hosted runners."
  Status: VERIFIED. GitHub Free private repos get 2,000 minutes/month.
- Claim: Docker Hub pull limits: 100 per 6 hours unauthenticated per IPv4 (or IPv6 /64), 200 for authenticated Personal, unlimited for paid; the stricter April 2025 limits were never enforced.
  URL: https://docs.docker.com/docker-hub/usage/pulls/ and https://www.docker.com/blog/revisiting-docker-hub-policies-prioritizing-developer-experience/
  Quote: "100 per IPv4 address or IPv6 /64 subnet"; "We did not enforce the Docker Hub rate limit changes previously scheduled for April 1, 2025."
  Status: VERIFIED. Also: "A pull for a multi-arch image will count as one pull for each different architecture." This argues for publishing the self-host image to ghcr.io and only mirroring to Docker Hub.
- Claim: Multi-arch builds with buildx.
  URL: https://docs.docker.com/build/building/multi-platform/
  Quote: "$ docker buildx build --platform linux/amd64,linux/arm64 ."
  Status: VERIFIED. Three strategies: QEMU emulation, multiple native nodes, cross-compilation in multi-stage builds.
- Claim: cosign keyless signing (Fulcio + Rekor); cosign v3.1.4 released 2026-10-20.
  URL: https://docs.sigstore.dev/cosign/signing/overview/ and https://github.com/sigstore/cosign/releases
  Quote: "Keyless signing associates identities, rather than keys, with an artifact signature."; "Fulcio issues short-lived certificates binding an ephemeral key to an OpenID Connect identity."; "Signing events are logged in Rekor, a signature transparency log."
  Status: VERIFIED.
- Claim: GitHub artifact attestations give SLSA v1.0 Build Level 2 out of the box, Level 3 with a reusable workflow; `actions/attest-build-provenance` v4 is a wrapper over `actions/attest`.
  URL: https://docs.github.com/en/actions/concepts/security/artifact-attestations , https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations , https://github.com/actions/attest-build-provenance
  Quote: "Artifact attestations by itself provides SLSA v1.0 Build Level 2."; permissions "id-token: write / contents: read / attestations: write"; "uses: actions/attest@v4" with "push-to-registry: true"; README: as of v4 it is "simply a wrapper on top of actions/attest"
  Status: VERIFIED. Public repos use the Sigstore Public Good instance (public transparency log); verify with `gh attestation verify ... -R org/repo`.
- Claim: SLSA build level definitions.
  URL: https://slsa.dev/spec/v1.0/levels
  Quote: L1 "Provenance showing how the package was built"; L2 "Signed provenance, generated by a hosted build platform"; L3 "Hardened build platform"
  Status: VERIFIED.
- Claim: SBOM generation: buildx `--sbom=true` (Syft-based), and GitHub attestations can carry an SBOM.
  URL: https://docs.docker.com/build/metadata/attestations/sbom/ and the artifact-attestations concept page above
  Quote: "pass the --attest type=sbom option to the docker buildx build command"; GitHub: "generate artifact attestations that include an associated software bill of materials (SBOM)"
  Status: VERIFIED.
- Claim: npm provenance and trusted publishing (OIDC).
  URL: https://docs.npmjs.com/generating-provenance-statements and https://docs.npmjs.com/trusted-publishers
  Quote: "npm publish --provenance"; "Today this includes GitHub Actions and GitLab CI/CD."; "Trusted publishing requires npm CLI version 11.5.1 or later and Node version 22.14.0 or higher."; with trusted publishing "provenance attestations are automatically generated for your packages without requiring the --provenance flag"
  Status: VERIFIED. Trusted publishing lists GitHub Actions (hosted runners), GitLab.com shared runners and CircleCI cloud; self-hosted runners "are not currently supported". No GA/beta label on the page.
- Claim: pnpm `minimumReleaseAge` was added in pnpm 10.16.0 (2025-09-12) and defaults to 1440 minutes since pnpm 11.
  URL: https://pnpm.io/settings/dependency-resolution
  Quote: "Added in: v10.16.0"; "Default: 1440 since v11, and 0 before v11"; it "applies to all dependencies, including transitive ones."
  Status: VERIFIED.
  URL: https://github.com/pnpm/pnpm/releases/tag/v10.16.0
  Quote: "It specifies the number of minutes that must pass after a version is published before pnpm will install it."
  Status: VERIFIED (registry publish time for 10.16.0: 2025-09-12).
  URL: https://github.com/pnpm/pnpm/releases/tag/v11.0.0
  Quote: "minimumReleaseAge defaults to 1 day (newly published packages are not resolved for 24h)"; "To opt out, set minimumReleaseAge: 0 in pnpm-workspace.yaml"
  Status: VERIFIED. pnpm 11 also sets `blockExoticSubdeps` and `strictDepBuilds` to true and requires `allowBuilds` for postinstall scripts. `minimumReleaseAgeExclude` (name-based since 10.16.0, patterns since 10.17.0, exact versions since 10.19.0) exempts packages.
- Claim: npm CLI v12 turns install scripts, git and remote dependencies off by default.
  URL: https://github.blog/changelog/2026-07-07-npm-install-time-security-and-gat-bypass2fa-deprecation
  Quote: "As of npm v12, the following npm install behaviors that used to run automatically are now opt-in."
  Status: VERIFIED. `allowScripts` off, `--allow-git none`, `--allow-remote none`; prepare with `npm approve-scripts --allow-scripts-pending`.

## 6. Supply-chain incidents 2025-2026

- Claim: 8 September 2025, maintainer "qix" phished via npmjs.help; chalk, debug and 17 more packages shipped a browser crypto-stealer.
  URL: https://socket.dev/blog/npm-author-qix-compromised-in-major-supply-chain-attack
  Quote: IOCs "chalk@5.6.1", "debug@4.4.2", "ansi-styles@6.2.2", "strip-ansi@7.1.1", "supports-color@10.2.1"; phishing sender "support@npmjs.help"; "the actor swaps any crypto transactions to their own address, redirecting any currency to their accounts."
  Status: VERIFIED.
- Claim: Mid-September 2025 Shai-Hulud: self-replicating worm starting at @ctrl/tinycolor, using TruffleHog and injected GitHub Actions workflows, spreading through stolen npm tokens.
  URL: https://www.stepsecurity.io/blog/ctrl-tinycolor-and-40-npm-packages-compromised
  Quote: "The malware repurposes open-source tools like TruffleHog to scan the filesystem for high-entropy secrets."; "The malware establishes persistence by injecting a GitHub Actions workflow file"
  Status: VERIFIED. The post's counts disagree with themselves ("over 500 NPM packages" in the intro, 195 rows in its table).
- Claim: Shai-Hulud 2.0 (November 2025) ran from a preinstall script and included a wiper.
  URL: https://socket.dev/blog/shai-hulud-strikes-again-v2
  Quote: "more than 500 packages and 700+ versions affected."; "Added a preinstall script setup_bun.js"; "Finds all writable user files, shreds them with single-pass overwrite"
  Status: VERIFIED (post dated Nov 24, 2025).
- Claim: GitHub's response plan (22 September 2025) and its recommended mitigations.
  URL: https://github.blog/security/supply-chain-security/our-plan-for-a-more-secure-npm-supply-chain/
  Quote: "On September 14, 2025, we were notified of the Shai-Hulud attack, a self-replicating worm that infiltrated the npm ecosystem"; publishing options reduced to "Local publishing with required two-factor authentication (2FA).", "Granular tokens which will have a limited lifetime of seven days.", "Trusted publishing."; also "Deprecate legacy classic tokens." and "Deprecate time-based one-time password (TOTP) 2FA, migrating users to FIDO-based 2FA."
  Status: VERIFIED. Actions for maintainers, quoted: "Use npm trusted publishing instead of tokens."; "Strengthen publishing settings on accounts, orgs, and packages to require 2FA for any writes and publishing actions."; "When configuring two-factor authentication, use WebAuthn instead of TOTP."
- Claim: Follow-through: classic tokens revoked 9 December 2025, write tokens capped at 90 days, two-hour login sessions.
  URL: https://github.blog/changelog/2025-12-09-npm-classic-tokens-revoked-session-based-auth-and-cli-token-management-now-available/
  Quote: "We've permanently revoked all existing npm classic tokens."; write tokens "limited to 90 days maximum"; "when you use npm login you'll receive a two-hour session token instead of long-lived tokens."
  Status: VERIFIED.
- Claim: 2026 incidents.
  URL: https://orca.security/resources/blog/tanstack-npm-supply-chain-worm/
  Quote: "poisoned the GitHub Actions cache with a malicious pnpm store."; "373 malicious package-version entries spanning 169 npm package names and 2 PyPI packages."
  Status: VERIFIED (disclosed 12 May 2026; TeamPCP "Mini Shai-Hulud"; the attack ran through a `pull_request_target` workflow and runner OIDC tokens, so trusted publishing alone did not stop it).
  URL: https://snyk.io/blog/node-gyp-supply-chain-compromise-self-propagating-npm-worm-binding-gyp/
  Quote: "57 affected packages across hundreds of malicious versions."; "Pin to known-good versions, run npm install --ignore-scripts, rotate all reachable credentials."
  Status: VERIFIED (published 2026-06-04; payload hidden in `binding.gyp`, not in lifecycle scripts; the post also lists `.claude/`, `.cursor/rules/` and `.vscode/tasks.json` as persistence hooks to remove).
  URL: https://securitylabs.datadoghq.com/articles/npm-worm-compromises-popular-npm-packages/
  Quote: began with "a malicious commit (174f6a5) in the GitHub repository of the popular keyv npm package"; "A valid attestation can faithfully bind a malicious source and an attacker-controlled build to the resulting package. It is neither forged nor a safety verdict."
  Status: VERIFIED (4 August 2026, "ChainDrop", keyv/cacheable family, hundreds of packages). Reported October 2026 Tensorlake SDK infection: search results only, page not opened, NOT FOUND as a verified source.
- Claim: Further GitHub hardening in 2026: bypass-2FA granular tokens lose account-management powers (August 2026) and lose direct publishing (about January 2027).
  URL: https://github.blog/changelog/2026-07-07-npm-install-time-security-and-gat-bypass2fa-deprecation
  Quote: "We expect this change to take effect in early August 2026."; "We expect this change to take effect around January 2027."
  Status: VERIFIED. The companion post https://github.blog/changelog/2026-07-31-restricting-npm-bypass-2fa-granular-access-tokens appeared in search results but was not opened.

## 7. Frontend options and CSP

- Claim: Vue's runtime-only build has no template compiler, so templates must be precompiled; the full build compiles in the browser.
  URL: https://vuejs.org/guide/scaling-up/tooling.html
  Quote: "Build files that start with vue.runtime.* are runtime-only builds: they do not include the compiler."; "Our default tooling setups use the runtime-only build since all templates in SFCs are pre-compiled."
  Status: VERIFIED.
- Claim: The in-browser compiler needs `'unsafe-eval'` because it builds render functions with `new Function`.
  URL: https://raw.githubusercontent.com/vuejs/core/main/packages/vue/src/index.ts
  Quote: `new Function('Vue', code)(runtimeDom)`
  Status: VERIFIED in source. The Vue 3 docs pages opened (security, tooling, package README) do not say "CSP" or "unsafe-eval"; the explicit sentence lives in the Vue 2 docs (https://v2.vuejs.org/v2/guide/installation.html): "The full build depends on this feature to compile templates, so is unusable in these environments. On the other hand, the runtime-only build is fully CSP-compliant." Status for the Vue 3 docs claim: PARTIAL.
- Claim: Vite supports CSP nonces via `html.cspNonce` and warns about `data:` URIs.
  URL: https://vite.dev/guide/features and https://vite.dev/config/shared-options
  Quote: "When html.cspNonce is set, Vite adds a nonce attribute with the specified value to any <script> and <style> tags"; "Ensure that you replace the placeholder with a unique value for each request."; "Do not allow data: for script-src. It will allow injection of arbitrary scripts."
  Status: VERIFIED. For a static SPA served without per-request rendering, nonces cannot vary per request; use hashes or external scripts only and set `build.assetsInlineLimit: 0` to avoid `data:` in `img-src`/`font-src`.
- Claim: SvelteKit has built-in CSP config.
  URL: https://svelte.dev/docs/kit/configuration
  Quote: "'auto' will use hashes for prerendered pages, and nonces for dynamically rendered pages."; "Using nonces with prerendered pages is insecure and therefore forbidden."
  Status: VERIFIED.
- Claim: Nuxt supports pure SPA mode with `ssr: false`.
  URL: https://nuxt.com/docs/4.x/guide/concepts/rendering
  Quote: `ssr: false`; "We can host client-only applications on any static server with HTML, CSS, and JavaScript files."
  Status: VERIFIED (Nuxt 4.6.2 on npm, 2026-10-16).
- Reasoning (framework choice): The app has three screens and all content is decrypted in the browser, so the server never sees plaintext and SSR has nothing to render except a shell. Nuxt's listed SSR benefits (SEO, faster first paint of content) do not apply, and SSR on Workers would burn the 10 ms CPU budget per request and count against the 100,000 requests/day, while static assets are free and unlimited. A Vite SPA (Vue or Svelte, or no framework) is the right shape. Vanilla TypeScript is feasible at three screens: Web Crypto, `fetch`, `<template>` elements and a small router cover it, and it keeps the bundle tiny and the CSP strict (no runtime compiler, no eval). The costs are hand-written DOM updates and forms; if the screens have more than trivial state, Vue 3 with precompiled SFCs or Svelte 5 adds little weight and keeps CSP clean.

## 8. Go and Rust alternatives for the server

- Claim: Go 1.27.3 plus modernc.org/sqlite v1.60.1 (cgo-free, SQLite 3.53.4) gives a single static binary on Node-free images.
  URL: https://pkg.go.dev/modernc.org/sqlite and https://pkg.go.dev/modernc.org/sqlite?tab=versions
  Quote: "Package sqlite is a sql/database driver using a CGo-free port of the C SQLite3 library."; "v1.60.1" "Sep 29, 2026"
  Status: VERIFIED. 20 GOOS/GOARCH combinations listed.
- Claim: Go is not a first-class Workers language; it runs only as Wasm, in practice via TinyGo and the community `syumai/workers-go` package.
  URL: https://developers.cloudflare.com/workers/languages/ and https://developers.cloudflare.com/workers/runtime-apis/webassembly/
  Quote: "Workers is a polyglot platform, and provides first-class support for the following programming languages:" (JavaScript, TypeScript, Python, Rust); Wasm "allows you to compile languages like Rust, Go, or C to a binary format"; "WASI support is experimental on Cloudflare Workers, with only some syscalls implemented."
  Status: VERIFIED. `https://developers.cloudflare.com/workers/languages/go/` returns 404, so there is no official Go guide.
  URL: https://pkg.go.dev/github.com/syumai/workers-go
  Quote: "Caution: This is an experimental project."; "Go 1.24.0 or later"; "The TinyGo template requires TinyGo 0.42.0 or later."
  Status: VERIFIED. The Worker size limit is now 64 MiB uncompressed on both plans (section 3), so older "3 MB free / 10 MB paid" claims about Go binaries are obsolete; CPU time (10 ms on Free) and startup time (1 s) are the live constraints for a Wasm Go server.
- Claim: Rust 1.99.1 with axum 0.8.9 and rusqlite 0.40.2 for the container, and `worker` 0.8.7 (workers-rs) for Cloudflare.
  URL: https://crates.io/api/v1/crates/axum , https://crates.io/api/v1/crates/rusqlite , https://crates.io/api/v1/crates/worker
  Quote: axum "0.8.9" updated 2026-04-14; rusqlite "0.40.2" updated 2026-08-08 "Ergonomic wrapper for SQLite"; worker "0.8.7" updated 2026-09-25 "A Rust SDK for writing Cloudflare Workers."
  Status: VERIFIED.
- Claim: workers-rs status: usable but rough; axum works through the `http` feature; no Tokio.
  URL: https://github.com/cloudflare/workers-rs and https://developers.cloudflare.com/workers/languages/rust/
  Quote: "Expect a few rough edges, some unimplemented APIs, and maybe a bug or two here and there."; "The end result is being able to use frameworks like axum directly"; "no Tokio or async_std support."
  Status: VERIFIED. Cloudflare's Rust page says "Cloudflare Workers provides support for Rust via the workers-rs crate" with the `wasm32-unknown-unknown` target; R2 is not documented in the README bindings list (D1, KV, Durable Objects, Queues are). Rust can target the Free tier; the crypto library would then exist twice (Rust server, TS client/CLI/MCP), which breaks the "one shared client crypto library" goal.

## 9. MCP server SDK and Claude Code packaging

- Claim: `@modelcontextprotocol/sdk` latest is 1.32.1 (2026-10-05); the SDK has split into v2 packages `@modelcontextprotocol/server` and `@modelcontextprotocol/client`, both 2.3.1 (2026-10-05).
  URL: https://registry.npmjs.org/@modelcontextprotocol/sdk , https://registry.npmjs.org/@modelcontextprotocol/server , https://registry.npmjs.org/@modelcontextprotocol/client
  Quote: sdk `"latest": "1.32.1"`; server `"latest": "2.3.1"`; client `"latest": "2.3.1"`
  Status: VERIFIED.
- Claim: The TypeScript SDK supports stdio and Streamable HTTP, implements the 2026-07-28 spec, and runs on Node, Bun and Deno; v1 lives on a branch.
  URL: https://github.com/modelcontextprotocol/typescript-sdk
  Quote: "MCP server libraries (tools/resources/prompts, Streamable HTTP, stdio, auth helpers)"; "implementing the 2026-07-28 MCP spec"; "runs on Node.js, Bun, and Deno"; "v1.x (@modelcontextprotocol/sdk) lives on the" v1.x branch
  Status: VERIFIED. Middleware packages include `@modelcontextprotocol/hono`, which fits a Hono-based API.
- Claim: The 2026-07-28 spec defines exactly two standard transports.
  URL: https://modelcontextprotocol.io/specification/2026-07-28/basic/transports
  Quote: "stdio: newline-delimited messages over the standard streams of a client-launched subprocess."; "Streamable HTTP: each message is an HTTP POST to a single MCP endpoint; replies arrive as a JSON object or a request-scoped SSE stream."
  Status: VERIFIED.
- Claim: Claude Code CLI is `@anthropic-ai/claude-code` 2.1.306 (2026-10-21). Skills are `SKILL.md` directories following the open Agent Skills standard; plugins bundle skills, agents, hooks and MCP servers.
  URL: https://registry.npmjs.org/@anthropic-ai/claude-code , https://code.claude.com/docs/en/skills , https://agentskills.io/specification , https://code.claude.com/docs/en/plugins
  Quote: "Claude Code skills follow the Agent Skills open standard, which works across multiple AI tools."; spec: `name` "Max 64 characters. Lowercase letters, numbers, and hyphens only." and `description` "Max 1024 characters."; plugin manifest "a JSON file at .claude-plugin/plugin.json"
  Status: VERIFIED. Skill locations: `~/.claude/skills/<name>/SKILL.md` (personal), `.claude/skills/<name>/SKILL.md` (project), `<plugin>/skills/<name>/SKILL.md`. Outside Claude Code only `name`, `description`, `license`, `compatibility`, `metadata`, `allowed-tools` are allowed in frontmatter. Plugins install from a marketplace (`.claude-plugin/marketplace.json`) or from a folder with `--plugin-dir`; an `.mcp.json` in the plugin registers the MCP server.

## Version table

| Component | Version | Date shown | Source |
|---|---|---|---|
| Node.js 26 (Current) | 26.12.1 | first released 2026-05-05; Active LTS 2026-10-28; EOL 2029-04-30 | https://nodejs.org/en/about/previous-releases , nodejs/Release README |
| Node.js 24 (LTS Krypton) | 24.22.1 | Maintenance from 2026-10-20; EOL 2028-04-30 | same |
| Node.js 22 (Maintenance LTS Jod) | 22.23.4 | EOL 2027-04-30 | same |
| Node.js 25 | 25.9.0 | EOL 2026-03-31 | https://nodejs.org/en/about/previous-releases |
| TypeScript | 7.0.2 (7.1.0-beta) | 2026-07-08 (registry); 7.0 announced 2026-06-18 | registry.npmjs.org/typescript, devblogs.microsoft.com/typescript |
| Vite | 8.3.5 | 2026-10-21 | registry.npmjs.org/vite |
| Vue | 3.5.44 (3.6.0-rc.12) | 2026-10-13 | registry.npmjs.org/vue |
| Svelte / SvelteKit | 5.57.2 / 3.1.0 | 2026-10-06 / 2026-10-14 | registry.npmjs.org |
| Nuxt | 4.6.2 | 2026-10-16 | registry.npmjs.org/nuxt |
| Hono | 4.13.13 (5.0.0-rc.1) | 2026-10-04 (rc.1 2026-10-10) | registry.npmjs.org/hono, github.com/honojs/hono/releases |
| @hono/node-server | 2.1.5 | 2026-10-13 | registry.npmjs.org |
| Wrangler | 4.151.3 | 2026-10-20 | registry.npmjs.org/wrangler |
| workerd | 1.20261021.1 | 2026-10-21 | registry.npmjs.org/workerd |
| Bun | 1.4.3 | 2026-10-10 | github.com/oven-sh/bun/releases |
| Deno | 2.9.7 | 2026-09-16 | github.com/denoland/deno/releases |
| pnpm | 12.14.0 | 2026-10-15 | registry.npmjs.org/pnpm |
| Vitest | 5.0.5 | 2026-10-15 | registry.npmjs.org/vitest |
| Playwright | 1.64.1 | 2026-10-14 | registry.npmjs.org/playwright |
| better-sqlite3 | 13.0.3 | 2026-08-05 | registry.npmjs.org/better-sqlite3 |
| Litestream | v0.5.17 | 2026-08-31 | github.com/benbjohnson/litestream/releases |
| Go | go1.27.3 | 2026-10-19 | go.dev/doc/devel/release |
| Rust | 1.99.1 | 2026-10-15 | blog.rust-lang.org |
| modernc.org/sqlite | v1.60.1 (SQLite 3.53.4) | 2026-09-29 | pkg.go.dev |
| axum / rusqlite / worker | 0.8.9 / 0.40.2 / 0.8.7 | 2026-04-14 / 2026-08-08 / 2026-09-25 | crates.io API |
| Docker node image | 24.22.1 (bookworm/alpine3.24), 26.12.1 (trixie) | tags listed 2026-10-21 | hub.docker.com/_/node |
| Distroless Node | nodejs22/24/26-debian13 | n/a | github.com/GoogleContainerTools/distroless |
| cosign | v3.1.4 | 2026-10-20 | github.com/sigstore/cosign/releases |
| @modelcontextprotocol/sdk | 1.32.1 | 2026-10-05 | registry.npmjs.org |
| @modelcontextprotocol/server, /client | 2.3.1 | 2026-10-05 | registry.npmjs.org |
| @anthropic-ai/claude-code | 2.1.306 | 2026-10-21 | registry.npmjs.org |

## Decision-relevant summary

- Target Node 24 (Maintenance LTS as of yesterday, EOL 2028-04-30) for the container and CLI; Node 26 enters Active LTS on 2026-10-28, so `node:24-alpine` today with a planned move to `node:26-alpine`. Node 22 and 25 are not worth new work.
- `node:sqlite` is "Release candidate" (1.2) on Node 24 and 26, not yet Stable, and is a non-functional stub on Workers. better-sqlite3 13.0.3 is the conservative choice on Node; either way the Workers build needs a Durable Objects SQLite adapter, so design one storage interface with two implementations from day one.
- Durable Objects SQLite on Free: 5 GB account storage (plan for 1 GB per object), 2 MB per row/BLOB, 100 bound parameters, 100 KB statements, 100k rows written per day. Encrypted payloads above about 1 MB belong in R2, not in rows.
- R2 covers large files: 5 TiB objects, multipart in 5 MiB to 5 GiB parts, presigned PUT/GET up to 7 days, generated offline with SigV4.
- Workers Free: 100k requests/day, 10 ms CPU, 64 MiB bundle, 100 MB request body (account plan limit), 5 cron triggers. Static asset requests are free and unlimited, so a Vite SPA served by the same Worker with `not_found_handling = "single-page-application"` and `run_worker_first = ["/api/*"]` is the right layout.
- Hono 4.13.13 runs the identical `app` on Workers and on `@hono/node-server`. Hono 5 is at rc.1 with no notes yet; stay on 4.x. Hono ships Moderate advisories monthly, mostly in jsx/SSG/proxy helpers; keep `serveStatic`, CORS and `parseBody` patched (at least 4.13.11).
- SSR adds nothing when all content is decrypted client side; use `ssr: false`-style SPA (plain Vite, or Nuxt only if its conventions are wanted). Vanilla TypeScript is feasible for three screens; Vue with precompiled SFCs or Svelte 5 both keep CSP free of `unsafe-eval`. Avoid the Vue full build (it uses `new Function`).
- Rust (axum + rusqlite, workers-rs 0.8.7) can target the Free tier; Go only via experimental TinyGo/Wasm. Both would duplicate the crypto library already planned in TypeScript, so TypeScript on both targets remains the simpler path.
- Publish the self-host image to ghcr.io (free for public packages, built on free public-repo Actions minutes), multi-arch via `docker buildx build --platform linux/amd64,linux/arm64`, signed with `actions/attest@v4` (SLSA Build L2; L3 with a reusable workflow) plus `--sbom=true`. Mirror to Docker Hub only as a convenience: anonymous pulls are capped at 100 per 6 hours per IP and multi-arch counts per architecture.
- For the npm packages (CLI, MCP server, crypto lib): use npm trusted publishing (OIDC, npm CLI 11.5.1+, Node 22.14+), which emits provenance automatically; classic tokens are gone and write tokens are capped at 90 days.
- Supply-chain defaults for the repo: pnpm 12 with `minimumReleaseAge` left at 1440 (default since pnpm 11), `allowBuilds` allowlist, pinned lockfile. The 2026 waves (TanStack cache poisoning, node-gyp `binding.gyp`, ChainDrop with valid provenance) show that `--ignore-scripts` and provenance alone are not enough; a release-age delay plus lockfile pinning and credential rotation are the practical controls, and `pull_request_target` workflows and writable Actions caches need review.
- MCP: build on the v2 split packages (`@modelcontextprotocol/server` 2.3.1, spec 2026-07-28) with stdio for local Claude Code use and Streamable HTTP for remote; `@modelcontextprotocol/hono` exists if the MCP endpoint should share the Hono API. Package the Claude Code integration as a plugin (`.claude-plugin/plugin.json` + `skills/` + `.mcp.json`) following the Agent Skills frontmatter rules.

## URLs opened

- https://nodejs.org/en/about/previous-releases
- https://raw.githubusercontent.com/nodejs/Release/main/README.md
- https://registry.npmjs.org/{typescript,@typescript/native-preview,vite,vue,svelte,@sveltejs/kit,hono,@hono/node-server,wrangler,workerd,miniflare,@cloudflare/workers-types,@cloudflare/vite-plugin,pnpm,vitest,playwright,better-sqlite3,nuxt,@modelcontextprotocol/sdk,@modelcontextprotocol/server,@modelcontextprotocol/client,@anthropic-ai/claude-code} (registry API)
- https://github.com/microsoft/TypeScript/releases
- https://github.com/microsoft/typescript-go
- https://devblogs.microsoft.com/typescript/
- https://github.com/cloudflare/workers-sdk/releases
- https://github.com/honojs/hono/releases
- https://github.com/honojs/hono/releases/tag/v5.0.0-rc.1
- https://github.com/oven-sh/bun/releases
- https://github.com/denoland/deno/releases
- https://go.dev/dl/
- https://go.dev/doc/devel/release
- https://blog.rust-lang.org/
- https://github.com/rust-lang/rust/releases
- https://hub.docker.com/_/node
- https://images.chainguard.dev/directory/image/node/overview
- https://images.chainguard.dev/directory/image/node/versions
- https://github.com/GoogleContainerTools/distroless
- https://hono.dev/docs/
- https://hono.dev/docs/getting-started/cloudflare-workers
- https://hono.dev/docs/getting-started/nodejs
- https://github.com/honojs/hono/security/advisories
- https://github.com/advisories?query=hono
- https://github.com/advisories/GHSA-5r4p-p66f-jhc7
- https://github.com/advisories/GHSA-3g9q-qwv8-cxpf
- https://github.com/advisories/GHSA-f23p-vx2j-j53r
- https://developers.cloudflare.com/workers/runtime-apis/nodejs/
- https://developers.cloudflare.com/workers/runtime-apis/web-crypto/
- https://developers.cloudflare.com/durable-objects/api/sqlite-storage-api/
- https://developers.cloudflare.com/durable-objects/platform/limits/
- https://developers.cloudflare.com/durable-objects/platform/pricing/
- https://developers.cloudflare.com/r2/platform/limits/
- https://developers.cloudflare.com/r2/objects/multipart-objects/
- https://developers.cloudflare.com/r2/api/s3/presigned-urls/
- https://developers.cloudflare.com/workers/platform/limits/
- https://developers.cloudflare.com/workers/static-assets/
- https://developers.cloudflare.com/workers/static-assets/routing/single-page-application/
- https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/
- https://developers.cloudflare.com/workers/runtime-apis/bindings/rate-limit/
- https://developers.cloudflare.com/changelog/post/2025-09-19-ratelimit-workers-ga/
- https://developers.cloudflare.com/workers/configuration/cron-triggers/
- https://developers.cloudflare.com/workers/platform/deploy-buttons/
- https://developers.cloudflare.com/workers/ci-cd/builds/
- https://developers.cloudflare.com/workers/ci-cd/builds/limits-and-pricing/
- https://developers.cloudflare.com/workers/languages/
- https://developers.cloudflare.com/workers/languages/go/ (404)
- https://developers.cloudflare.com/workers/languages/rust/
- https://developers.cloudflare.com/workers/runtime-apis/webassembly/
- https://nodejs.org/docs/latest-v22.x/api/sqlite.html
- https://nodejs.org/docs/latest-v24.x/api/sqlite.html
- https://nodejs.org/docs/latest-v26.x/api/sqlite.html
- https://github.com/benbjohnson/litestream/releases
- https://litestream.io/
- https://litestream.io/guides/
- https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-packages/about-billing-for-github-packages
- https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-actions/about-billing-for-github-actions
- https://docs.docker.com/docker-hub/usage/
- https://docs.docker.com/docker-hub/usage/pulls/
- https://www.docker.com/blog/revisiting-docker-hub-policies-prioritizing-developer-experience/
- https://docs.docker.com/build/building/multi-platform/
- https://docs.docker.com/build/metadata/attestations/sbom/
- https://docs.sigstore.dev/cosign/signing/overview/
- https://github.com/sigstore/cosign/releases
- https://github.com/actions/attest-build-provenance
- https://docs.github.com/en/actions/concepts/security/artifact-attestations
- https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations
- https://slsa.dev/spec/v1.0/levels
- https://docs.npmjs.com/generating-provenance-statements
- https://docs.npmjs.com/trusted-publishers
- https://pnpm.io/settings
- https://pnpm.io/settings/dependency-resolution
- https://github.com/pnpm/pnpm/releases/tag/v10.16.0
- https://github.com/pnpm/pnpm/releases/tag/v11.0.0
- https://github.blog/security/supply-chain-security/our-plan-for-a-more-secure-npm-supply-chain/
- https://github.blog/changelog/2025-12-09-npm-classic-tokens-revoked-session-based-auth-and-cli-token-management-now-available/
- https://github.blog/changelog/2026-07-07-npm-install-time-security-and-gat-bypass2fa-deprecation
- https://github.blog/news-insights/product-news/new-npm-features-for-secure-publishing-and-safe-consumption/ (2022 post, not relevant)
- https://socket.dev/blog/npm-author-qix-compromised-in-major-supply-chain-attack
- https://www.stepsecurity.io/blog/ctrl-tinycolor-and-40-npm-packages-compromised
- https://socket.dev/blog/shai-hulud-strikes-again-v2
- https://snyk.io/blog/node-gyp-supply-chain-compromise-self-propagating-npm-worm-binding-gyp/
- https://securitylabs.datadoghq.com/articles/npm-worm-compromises-popular-npm-packages/
- https://orca.security/resources/blog/tanstack-npm-supply-chain-worm/
- https://vuejs.org/guide/best-practices/security.html
- https://vuejs.org/guide/scaling-up/tooling.html
- https://github.com/vuejs/core/blob/main/packages/vue/README.md
- https://raw.githubusercontent.com/vuejs/core/main/packages/vue/src/index.ts
- https://v2.vuejs.org/v2/guide/installation.html
- https://vite.dev/guide/features
- https://vite.dev/config/shared-options
- https://svelte.dev/docs/kit/configuration
- https://nuxt.com/docs/4.x/guide/concepts/rendering
- https://pkg.go.dev/modernc.org/sqlite
- https://pkg.go.dev/modernc.org/sqlite?tab=versions
- https://pkg.go.dev/github.com/syumai/workers-go
- https://github.com/syumai/workers-go
- https://crates.io/api/v1/crates/{rusqlite,axum,worker} (API)
- https://github.com/cloudflare/workers-rs
- https://github.com/modelcontextprotocol/typescript-sdk
- https://modelcontextprotocol.io/specification/2026-07-28/basic/transports
- https://code.claude.com/docs/en/skills
- https://code.claude.com/docs/en/plugins
- https://agentskills.io/specification
