# Repository map

| Path                                                                                                                | Purpose                                                                                  |
| ------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| [spec/vectors.json](../../../spec/vectors.json)                                                                     | Shared protocol test vectors                                                             |
| [cmd/nylzo-vectors](../../../cmd/nylzo-vectors)                                                                     | Generator of the shared protocol test vectors                                            |
| [internal/core](../../../internal/core), [web/core](../../../web/core)                                              | Go and TypeScript protocol implementations                                               |
| [internal/pepper](../../../internal/pepper)                                                                         | Purpose-separated server keys and pepper rotation                                        |
| [internal/seal](../../../internal/seal)                                                                             | Sealing of stored records: per-purpose keys, locators and padding                        |
| [internal/store](../../../internal/store)                                                                           | SQLite and file-storage adapters and shared storage tests                                |
| [internal/server](../../../internal/server)                                                                         | HTTP endpoints, limits, proof of work, headers, cleanup, and embedded client             |
| [cmd/nylzo](../../../cmd/nylzo), [internal/cli](../../../internal/cli), [internal/client](../../../internal/client) | CLI, server commands, and HTTP client                                                    |
| [internal/buildinfo](../../../internal/buildinfo)                                                                   | Version and commit data reported by the CLI and the server                               |
| [web/app](../../../web/app)                                                                                         | Svelte web client with English and Czech interfaces                                      |
| [web/e2e](../../../web/e2e)                                                                                         | Playwright tests, including CLI/browser interoperability                                 |
| [skills/nylzo](../../../skills/nylzo)                                                                               | Agent Skills instructions for using the CLI                                              |
| [deploy/compose](../../../deploy/compose)                                                                           | Self-hosting example with Caddy and Docker secrets                                       |
| [deploy/hetzner-flatcar](../../../deploy/hetzner-flatcar)                                                           | Hetzner Flatcar provisioning, signed self-updates and host policy                        |
| [assets/brand](../../../assets/brand)                                                                               | Official light/dark logos and brand colors                                               |
| [.github/](../../../.github), [.goreleaser.yaml](../../../.goreleaser.yaml)                                         | CI, community forms, dependency updates, and release configuration                       |
| [automation](../../../automation)                                                                                   | Typed scripts for Renovate, the dependency guard, release tags and scans                 |
| [tools](../../../tools), [mise-tasks](../../../mise-tasks)                                                          | Build, CI routing, coverage, release and deploy scripts and the mise tasks that run them |
| [security](../../../security)                                                                                       | Dependency audit policy and OSV-Scanner configuration                                    |
| [docs/wiki/](..)                                                                                                    | Reviewed sources for the public Wiki                                                     |

Start with [Development](../guides/Development.md) to run the project or [Contributing](../community/Contributing.md) to submit a change.
