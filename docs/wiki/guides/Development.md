# Development

Start with [CONTRIBUTING.md](../community/Contributing.md). Use Linux, macOS, or WSL with a C compiler for Go's race detector. [mise.toml](../../../mise.toml) pins the tools; [mise.lock](../../../mise.lock) records their checksums. Inspect both files and the repository scripts before running `mise trust`.

## Local server and hot reload

```sh
mise trust
mise install
mise run dev
```

The server listens on `http://localhost:8080`. Local state lives in `.dev/`. Creation and read rate limits are relaxed for development. Set `NYLZO_DEV_PORT=8081` to use another port; browser tests use 8090.

For hot reload, run this in a second terminal:

```sh
mise run dev:web
```

Open `http://localhost:5173`. Vite proxies `/api` to the local development server; `NYLZO_DEV_API` overrides that address. Test production CSP and nonces through the embedded client or browser tests, because Vite does not serve the production CSP. A plain HTTP LAN address does not provide the secure context Web Crypto requires; use HTTPS or localhost.

`mise run dev:reset` **deletes** local messages and keys in `.dev/`.

## Build and test

```sh
mise run test
mise run lint
mise run build
mise run e2e:browsers
mise run e2e
```

`test` runs Go with the race detector and the TypeScript suites. `lint` runs Go, TypeScript, workflow, and shell checks. `mise tasks` lists all tasks.

`mise run build` embeds the production web client in `bin/nylzo` and injects the same build identity into Go and Vite. Untagged and modified checkouts identify themselves as development builds. See [versioning](../maintainers/Releasing.md#one-source-of-version-identity).

## Which CI checks run?

CI selects test and build jobs from changed paths on pull requests and pushes to `main`. [dorny/paths-filter](https://github.com/dorny/paths-filter) reads the Git diff, including deleted paths and both sides of renames. Every run validates the Wiki links and the routing rules, then reports its selected jobs in the Actions summary.

| Change                                                                                                   | Checks                                                                          |
| -------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Wiki, README, community Markdown, README artwork                                                         | Wiki validation and routing regression tests                                    |
| Go server, CLI, or storage                                                                               | Go checks, race tests, timing tests, fuzzing, Go CodeQL, integration and builds |
| Web app, browser tests, or embedded web assets                                                           | Web checks, JavaScript/TypeScript CodeQL, integration and builds                |
| Deployment configuration                                                                                 | Workflow/script checks, integration and builds                                  |
| Protocol, vectors, either cryptographic core, dependencies, CI, toolchain, build tools, or unknown paths | Full verification                                                               |

Code, build, configuration, and dependency changes also run the required SCA job against both lockfiles, including known-malware and license checks. It retains raw findings and import evidence; exact non-affected findings have a bounded VEX assessment. See [security control policies](../maintainers/Security-controls.md).

Every pull request also runs all three CodeQL analyses. The repository requires CodeQL results before merge; skipping them leaves even a documentation-only PR blocked. On pushes to `main`, CodeQL runs only for the affected languages.

Integration includes Chromium, Firefox, WebKit, Compose, developer setup, and reproducible release archives. Mixed changes combine their checks. `Protocol.md` remains a full-verification input because Go and browser tests read its CSP contract.

Selection happens at the job level. The workflow itself always starts: GitHub can leave required checks pending when a workflow is skipped by a path filter. See [GitHub's required-check guidance](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks).

`CI required` runs even when other jobs are skipped. It rejects failed classification and failed, cancelled, missing, or unexpectedly skipped jobs. The weekly schedule and a manual run always select the full suite, including all three CodeQL languages. To request one on `main`:

```sh
gh workflow run ci.yml --ref main
```

A successful full run on `main` produces `Release verification`. A documentation-only run cannot qualify a commit for release; see [Releasing](../maintainers/Releasing.md).

## Protocol vectors and memory

After a protocol-core change, regenerate the shared vectors:

```sh
go run ./cmd/nylzo-vectors -out spec/vectors.json
```

The Go generator and TypeScript implementation must agree with the committed vectors. The largest RFC 7914 scrypt vector needs 1 GiB of memory. TypeScript runs it with `NYLZO_HEAVY_TESTS=1`, enabled in CI. `go test -short` skips its recalculation in the core; the vector generator still computes it.

The web app and ESLint use the TypeScript version in [web/package.json](../../../web/package.json) for tool compatibility; the core and browser tests pin theirs separately. Use the checked-in package manager and lockfile rather than updating tools implicitly.

## Section links

Home-page section links use English fragments in both languages, such as `#create`, `#how-it-works`, and `#security`. Earlier Czech fragments remain as invisible anchors so shared links keep working, including before JavaScript loads. Message-link fragments keep their protocol-defined format.
