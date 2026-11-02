# Bun toolchain

Bun 1.4.4 replaces Node.js and pnpm for project scripts, dependency installation, Vite, Vitest, Playwright, repository tooling, CI, release builds, deployment tooling, and monitors. The distributed Nylzo service remains a Go executable with an embedded static client. JavaScript GitHub Actions and the upstream Renovate container own their internal runtimes independently of project scripts.

## Reproducible installation

`mise.toml` and `mise.lock` pin Bun and its downloads. Both JavaScript workspaces declare the same `packageManager` and engine. `web/package.json` declares the `core`, `app`, and `e2e` workspaces; `automation` stays independent so the dependency scanner can bootstrap without importing unvetted automation dependencies.

Run `mise run install`. CI and releases use `bun install --frozen-lockfile --ignore-scripts`. The committed text `bun.lock` files replace pnpm locks. Each workspace has isolated dependency installation, exact direct versions, a 24-hour `minimumReleaseAge` in seconds, no quarantine exclusions, and `trustedDependencies: []`. This empty list replaces Bun's built-in trust list; dependency install scripts remain forbidden. The install-policy check enforces these settings. Bun's age gate applies when resolving new versions, rather than reevaluating an existing lockfile on every install.

The reviewed deepmerge patch remains byte-for-byte unchanged. Installation checks its SHA-256, manifest and lockfile binding, all installed patched source hashes, prototype-poisoning cases, and Svelte option-merging behavior. OSV-Scanner reads both Bun locks, preserving the npm ecosystem, license inventory, SBOM and scoped VEX evidence. The dependency guard rejects unexpected lock settings, trust changes, external package sources, missing integrity and unapproved workspace links. Historical pnpm review fixtures remain for maintenance branches.

## Runtime and tests

`[run] bun = true` in the committed Bun configuration overrides Node shebangs in package scripts. Leaf workspace commands explicitly use `bun run --bun` as well, so direct `--cwd` invocations cannot fall back to Node shebangs. Use `bun run test`, rather than `bun test`, to invoke the web's Vitest suite. Use `bun run test:coverage` for instrumented tests; the parallel task launcher does not forward coverage flags, so coverage is part of each workspace's named command. Automation and repository tools use Bun's native test runner with file isolation; the existing `node:test` imports use Bun's compatibility API and do not require a Node installation. Compatible `node:` standard-library imports remain useful and are supported by Bun.

Vitest remains the web runner because its Vite/Svelte transforms, module mocks and Istanbul coverage are part of the test contract. The compression suite runs in headless Chromium through Vitest's Playwright provider. Bun's DecompressionStream accepts concatenated gzip frames that browsers must reject; the browser suite preserves every compression assertion on the actual target platform. `mise run test:web` installs Chromium; CI also installs its system dependencies. The existing end-to-end matrix still checks Chromium, Firefox and WebKit.

API unit tests inspect the Fetch options supplied by the client directly: Bun's server-side Request getters do not model browser credentials/referrer policy. Attachment tests assert that the original file MIME type is preserved, since Bun may append a charset to a File's type. Browser behavior remains covered by end-to-end tests.

## Type checking and lint policy

All compiler commands use native TypeScript 7.0.2, including `svelte-check --tsgo`. Per Svelte's documented setup, the app also retains TypeScript 6.0.3 for the compiler API and installs TypeScript 7 as `@typescript/native`. Do not remove that compatibility dependency until Svelte supports doing so.

Oxlint enables the correctness, suspicious, pedantic and performance categories as errors, with TypeScript, Unicorn, Oxc, import, promise, Node, JSDoc and test rules. Compatible regexp, directive-comment, Perfectionist and Playwright JS plugins preserve checks that have no native counterpart. Style rules are selected explicitly because enabling every style rule introduces contradictory export/import conventions and arbitrary bans on the project's patterns. CI denies warnings and unused suppressions; suppressions require descriptions. Format and lint with `bun run --cwd web format` and `bun run --cwd web lint`, or the corresponding automation commands.

Fix code first, then inspect rule options, then consider a narrowly documented exception. Current adaptations include:

- Type-aware `require-await` handles Promise-forwarding APIs instead of the syntax-only duplicate. TypeScript's `noImplicitReturns` handles exhaustive control flow; the redundant consistent-return lint produced false positives for exhaustive switches and intentional undefined results even after adjusting its options.
- Readonly parameters remain required for consumed data. Platform handles, cryptographic buffers that must be wiped, explicit mutable accumulators and test doubles retain their actual contracts. Fault-injection tests may construct intentionally invalid values.
- Serialized protocol loops, bounded streaming reads, retry loops and destructive host operations must run in order. They cannot safely be rewritten as Promise.all solely to satisfy no-await-in-loop.
- File/function limits allow existing private-state factories, translation dictionaries and test suites while retaining bounds for other code. Named exports and formatter-owned sorting follow the existing module architecture. Oxfmt owns numeric literal casing because the native Unicorn rule requires uppercase hex digits while the formatter requires lowercase and that rule has no casing option.
- Vite's generated worker URL exports, missing TrustedScriptURL overloads and browser-serialized test closures have local, explained suppressions. Global checks stay enabled. Startup intentionally remains asynchronous without top-level await: module evaluation must finish independently of WebKit font readiness. The host monitor recognizes only the exact certificate-required TLS alert under both OpenSSL and BoringSSL names; server trust errors remain failures.

Oxfmt owns formatting, import sorting, package.json ordering and Tailwind class sorting. Its Svelte/HTML implementation currently delegates to bundled Prettier-compatible code, executed under Bun; there is no separate project Prettier command or dependency. Oxlint currently lints Svelte script blocks, not templates. The official Svelte autofixer and warning-failing Svelte check remain, and a Svelte AST policy preserves explicit button types, TypeScript script blocks, and bans on raw HTML, HTML bindings, inline styles and const tags.

## Official references

- [Bun runtime and forced Bun execution](https://bun.sh/docs/runtime)
- [Bun install, isolated dependencies and release age](https://bun.sh/docs/pm/cli/install)
- [Bun lifecycle-script trust](https://bun.sh/docs/pm/lifecycle)
- [Bun patches](https://bun.sh/docs/pm/cli/patch)
- [Bun Node.js compatibility](https://bun.sh/docs/runtime/nodejs-compat)
- [Oxlint configuration](https://oxc.rs/docs/guide/usage/linter/config)
- [Type-aware Oxlint](https://oxc.rs/docs/guide/usage/linter/type-aware)
- [Oxlint JS plugins and parser limits](https://oxc.rs/docs/guide/usage/linter/js-plugins)
- [Oxfmt language support](https://oxc.rs/docs/guide/usage/formatter/language-support)
- [Oxfmt configuration and Tailwind sorting](https://oxc.rs/docs/guide/usage/formatter/config-file-reference)
- [TypeScript 7](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/)
- [Svelte check and native TypeScript setup](https://github.com/sveltejs/language-tools/tree/master/packages/svelte-check)
- [Vitest browser mode](https://vitest.dev/guide/browser/)
- [OSV-Scanner lockfile support](https://google.github.io/osv-scanner/supported-languages-and-lockfiles/)
- [Renovate Bun manager](https://docs.renovatebot.com/modules/manager/bun/)
