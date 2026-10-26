# Contributing to Nylzo

Contributions are welcome: bug reports, documentation, tests, accessibility improvements, and code. The project is preparing for its first release. Discuss substantial changes before implementing them, especially changes to cryptography, the protocol, storage, or the release pipeline.

## Choose the right channel

- Report reproducible bugs and propose specific features using the [issue forms](https://github.com/misaon/nylzo/issues/new/choose).
- Ask questions and discuss ideas in [Discussions](https://github.com/misaon/nylzo/discussions).
- Report vulnerabilities **privately** through [GitHub Security Advisories](https://github.com/misaon/nylzo/security/advisories/new), following [SECURITY.md](SECURITY.md).

Read the [code of conduct](CODE_OF_CONDUCT.md). Use English for issues, pull requests, code, and contributor documentation. Use synthetic data in reproductions; never include real message links, keys, passphrases, credentials, or personal data.

## Set up a development environment

Use Linux, macOS, or WSL with a C compiler for Go's race detector. The exact versions of Go, Node.js, pnpm, and the CI tools are pinned in [mise.toml](mise.toml) and [mise.lock](mise.lock).

1. Fork this repository and create a branch from `main`.
2. Install [mise](https://mise.jdx.dev/getting-started.html).
3. Read `mise.toml` and `mise-tasks/` before trusting them: repository tasks execute code on your machine.
4. Run:

   ```sh
   mise trust
   mise install
   mise run dev
   ```

The development server listens on `http://localhost:8080`. For hot reload, run `mise run dev:web` in another terminal and open `http://localhost:5173`. Local data and keys live in `.dev/`; `mise run dev:reset` deletes them.

## Make and verify a change

The normative protocol is [spec/protocol.md](spec/protocol.md); the Go and TypeScript cores must agree on [spec/vectors.json](spec/vectors.json). Keep changes focused and use the protocol's terminology. Keep tests next to the code and add meaningful coverage for security properties, protocol behavior, and regressions.

```sh
mise run test
mise run lint
mise run build
```

For browser or client/server changes, also run:

```sh
mise run e2e:browsers
mise run e2e
```

On Linux, Playwright may also need system libraries (`pnpm --dir web/e2e exec playwright install-deps`, with the privileges your package manager requires). Include the commands you ran and any limitations in the pull request. CI runs additional fuzz, reproducibility, and deployment checks.

Do not hand-edit generated protocol vectors or dependency lockfiles. Regenerate vectors with `go run ./cmd/nylzo-vectors -out spec/vectors.json`; change dependencies deliberately with the pinned package manager and review the resulting lockfile diff. Do not relax dependency build-script restrictions or security checks to make a change pass.

## Open a pull request

Use an English Conventional Commit title, such as `feat(cli): add JSON version output`, and a short body explaining the problem, resulting behavior, and validation. Allowed types are `feat`, `fix`, `perf`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `revert`, `style`, and `security`. Add `!` before the colon for a breaking change and explain its migration in the body. The title becomes the squash commit and determines its section in the generated release notes. Link the relevant issue and acceptance criterion when applicable. Documentation-only changes do not need invented test coverage; describe how you checked them.

`main` requires a pull request, successful checks, resolved review conversations, and verified signatures on the resulting commits. Code-owner review is required for community changes; new commits dismiss previous approvals. The maintainer uses squash merges, so contributors do not need to configure commit signing to submit a pull request. The sole repository administrator can bypass the review requirement through a pull request for their own maintenance work; the separate integrity and CI rules still apply. This exception is documented in [repository governance](docs/github-governance.md).

Workflows from **all external contributors** require maintainer approval before running. Approval is a decision to execute the proposed code, so it can take time. Maintainers must inspect the complete diff, workflow changes, and executable setup files first. Never ask a maintainer to run a command with secrets or to enable a privileged workflow for your branch.

Only the maintainer publishes releases. Contributors should not create release tags or upload release assets. See the [release checklist](docs/release-checklist.md).

## License

By submitting a contribution, you agree that it may be distributed under the repository's [MIT license](LICENSE). Submit only work you have the right to contribute. The license does not grant trademark rights to the Nylzo name or logo.
