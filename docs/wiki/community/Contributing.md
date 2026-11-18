# Contributing

This page is for people who want to change Nylzo: how to set up the project, run the checks CI runs and get a pull request merged.

## Before you start

- Report a bug or propose a feature with the [issue forms](https://github.com/misaon/nylzo/issues/new/choose). Ask questions in [Discussions](https://github.com/misaon/nylzo/discussions).
- Report a vulnerability privately, as the [security policy](Security-policy.md) describes.
- Discuss changes to cryptography, the protocol, storage or the release pipeline before you write them.
- Write issues, pull requests, code and documentation in English. Use synthetic data: no real message links, passphrases, keys or personal data.
- Follow the [code of conduct](../../../CODE_OF_CONDUCT.md).

## Set up

You need Linux, macOS or WSL, and a C compiler for Go's race detector. [mise](https://mise.jdx.dev/getting-started.html) installs every other tool at the version pinned in [mise.toml](../../../mise.toml), with checksums in [mise.lock](../../../mise.lock). Tasks run code on your machine, so read `mise.toml` and [mise-tasks](../../../mise-tasks) before you trust them.

```sh
mise trust
mise install
mise run dev
```

`mise run dev` builds `bin/nylzo` and serves it on `http://localhost:8080` with relaxed rate limits. Its messages and pepper (the server's secret key) live in `.dev/`. Set `NYLZO_DEV_PORT` to use another port. `mise run dev:reset` deletes `.dev/`.

For hot reload, run Vite in a second terminal and open `http://localhost:5173`:

```sh
mise run dev:web
```

Vite forwards `/api` to `http://127.0.0.1:8080` (`NYLZO_DEV_API` changes it). It does not send the production Content Security Policy, so test CSP changes with `mise run dev` or the browser tests.

Bun runs every JavaScript and TypeScript tool; you need no Node.js or pnpm. `mise run install` installs the locked dependencies without running their install scripts.

## Build, lint and test

| Command | What it does |
| --- | --- |
| `mise run build` | Builds `bin/nylzo` with the web client embedded |
| `mise run test` | Go race tests, TypeScript core and web client tests, tests of `automation/` and `tools/` |
| `mise run lint` | Dependency scan, Go checks, Oxfmt, Oxlint, type checks, Svelte autofixer, workflow and shell checks |
| `mise run e2e:browsers` | Downloads the Playwright browsers once |
| `mise run e2e` | End-to-end tests in Chromium, Firefox and WebKit |

`mise tasks` lists the smaller tasks, such as `test:go` or `lint:web`. CI runs the same `lint:*` and `test:*` tasks. On Linux, Playwright may also need system libraries:

```sh
bun run --cwd web/e2e --bun playwright install-deps
```

Format the web workspace with `bun run --cwd web format`. In `web/`, `bun run test` starts Vitest; `bun test` would start Bun's own runner.

After a change to a protocol core, regenerate the shared test vectors. The Go and TypeScript cores must both match them.

```sh
go run ./cmd/nylzo-vectors -out spec/vectors.json
```

The largest scrypt vector needs 1 GiB of memory. `mise run test` runs it because it sets `NYLZO_HEAVY_TESTS=1`.

Do not edit the vectors or lockfiles by hand. Change dependencies with `go get` or `bun add`, then review the lockfile diff. Do not weaken install-script rules or security checks to make a change pass.

## Repository map

| Path | Contents |
| --- | --- |
| [internal/core](../../../internal/core), [web/core](../../../web/core) | Go and TypeScript protocol cores |
| [spec/vectors.json](../../../spec/vectors.json), [cmd/nylzo-vectors](../../../cmd/nylzo-vectors) | Shared test vectors and their generator |
| [internal/core/api](../../../internal/core/api) | Paths, headers, error codes and bodies shared by server, CLI and Caddy |
| [internal/server](../../../internal/server) | HTTP server, limits, admin area and embedded web client |
| [internal/store](../../../internal/store), [internal/seal](../../../internal/seal) | Storage and sealed records |
| [cmd/nylzo](../../../cmd/nylzo), [internal/cli](../../../internal/cli), [internal/client](../../../internal/client) | The `nylzo` command and its HTTP client |
| [web/app](../../../web/app), [web/e2e](../../../web/e2e) | Svelte web client and Playwright tests |
| [deploy](../../../deploy) | Compose example, Caddy build and the Hetzner Flatcar profile |
| [automation](../../../automation) | Scripts for Renovate, the Dependency guard, release tags and scans |
| [tools](../../../tools), [mise-tasks](../../../mise-tasks) | Build, CI routing, release and deploy scripts |
| [security](../../../security) | Dependency scan policy |
| [docs/wiki](..) | Sources of this Wiki |

## Conventions

- Use the words of the [protocol](../reference/Protocol.md) (message, envelope, lease, verifier, tombstone) and spell names out.
- Put tests next to the code. Test protocol vectors, security guarantees, spec rules and regressions; a test must fail when the guarantee breaks.
- Comment code only to state a constraint the code cannot show. A lint suppression names its rule and reason.
- Give the pull request an English [Conventional Commit](https://www.conventionalcommits.org/) title, such as `feat(cli): add JSON version output`. The types are `feat`, `fix`, `perf`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `revert`, `style` and `security`. Add `!` before the colon for a breaking change and describe the migration in the body. The title becomes the squash commit and picks its section in the release notes.
- Sign every commit with a key registered on GitHub. `main` and `release/v*` accept only commits with verified signatures.

## Get a pull request merged

1. Fork the repository, branch from `main` and open the pull request against `main`. The template asks what changed and which checks you ran.
2. GitHub holds the workflow runs of every outside contributor until a maintainer has read the diff and approves them.
3. CI picks its jobs from the changed paths. A change to the Wiki or the root Markdown files runs the link check and CodeQL only.
4. These checks must pass: `CI required`, `Conventional PR title`, `Dependency guard`, `Socket Security: Pull Request Alerts`, and CodeQL with no errors and no security alert of medium or higher severity.
5. A code owner approves the latest push and reruns `Dependency guard`, which passes an outside pull request only after that approval. Review conversations must be resolved.
6. The maintainer squash merges. Merging releases nothing; see [Releasing](../maintainers/Releasing.md).

Wiki pages are edited in `docs/wiki/` through a pull request. The maintainer publishes them after the merge.

## License

Contribute only work you may license under the [MIT license](../../../LICENSE). [Section D.6 of the GitHub Terms of Service](https://docs.github.com/en/site-policy/github-terms/github-terms-of-service#6-contributions-under-repository-license) applies; there is no separate CLA or DCO. The license grants no rights to the Nylzo name or logo; see [Brand](../guides/Brand.md).
