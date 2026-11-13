# Repository map

| Path                                                                                                                | Purpose                                                                                  |
| ------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| [spec/vectors.json](../../../spec/vectors.json)                                                                     | Shared protocol test vectors                                                             |
| [cmd/nylzo-vectors](../../../cmd/nylzo-vectors)                                                                     | Generator of the shared protocol test vectors                                            |
| [internal/core](../../../internal/core), [web/core](../../../web/core)                                              | Go and TypeScript protocol cores: link, envelope, keys, request signing, proof of work   |
| [internal/core/api](../../../internal/core/api)                                                                     | Wire protocol shared by server, CLI and Caddy: paths, headers, error codes, bodies       |
| [internal/vectors](../../../internal/vectors)                                                                       | Code behind the vector generator, kept apart from the protocol core                      |
| [internal/pepper](../../../internal/pepper)                                                                         | Purpose-separated server keys and pepper rotation                                        |
| [internal/seal](../../../internal/seal)                                                                             | Sealing of stored records: per-purpose keys, locators and padding                        |
| [internal/epoch](../../../internal/epoch)                                                                           | Per-minute epoch ratchet whose erased keys end messages at their deadline                |
| [internal/erase](../../../internal/erase), [internal/iolimit](../../../internal/iolimit)                            | In-place erasure of files and the process-wide limit on concurrent file I/O              |
| [internal/secretmode](../../../internal/secretmode)                                                                 | Runs request cryptography in Go's runtime/secret mode where the build supports it        |
| [internal/store](../../../internal/store)                                                                           | SQLite and file-storage adapters and shared storage tests                                |
| [internal/server](../../../internal/server)                                                                         | HTTP endpoints, limits, proof of work, headers, cleanup, and embedded client             |
| [cmd/nylzo](../../../cmd/nylzo), [internal/cli](../../../internal/cli), [internal/client](../../../internal/client) | CLI with `cli/messages`, `cli/operator`, `cli/verifyclient` and `cli/local`; HTTP client |
| [internal/localclient](../../../internal/localclient)                                                               | Verified local client: embedded web client on loopback, API relay and link handoff       |
| [internal/buildinfo](../../../internal/buildinfo)                                                                   | Version and commit data reported by the CLI and the server                               |
| [web/app](../../../web/app)                                                                                         | Svelte web client in 12 languages, with Czech as the source text                                      |
| [web/e2e](../../../web/e2e)                                                                                         | Playwright tests, including CLI/browser interoperability                                 |
| [skills/nylzo](../../../skills/nylzo)                                                                               | Agent Skills instructions for using the CLI                                              |
| [deploy/compose](../../../deploy/compose)                                                                           | Self-hosting example with Caddy and Docker secrets                                       |
| [deploy/caddy](../../../deploy/caddy)                                                                               | Caddy build that each release signs and ships                                            |
| [deploy/hetzner-flatcar](../../../deploy/hetzner-flatcar)                                                           | Hetzner Flatcar provisioning, signed self-updates and host policy                        |
| [assets/brand](../../../assets/brand)                                                                               | Official light/dark logos and brand colors                                               |
| [.github/](../../../.github), [.goreleaser.yaml](../../../.goreleaser.yaml)                                         | CI, community forms, dependency updates, and release configuration                       |
| [automation](../../../automation)                                                                                   | Typed scripts for Renovate, the dependency guard, release tags and scans                 |
| [tools](../../../tools), [mise-tasks](../../../mise-tasks)                                                          | Build, CI routing, coverage, release and deploy scripts and the mise tasks that run them |
| [security](../../../security)                                                                                       | Dependency audit policy and OSV-Scanner configuration                                    |
| [docs/wiki/](..)                                                                                                    | Reviewed sources for the public Wiki                                                     |

Start with [Development](../guides/Development.md) to run the project or [Contributing](../community/Contributing.md) to submit a change.
