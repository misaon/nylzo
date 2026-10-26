# Repository map

| Path                                                                                                       | Purpose                                                                       |
| ---------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| [spec/](../../spec)                                                                                        | Normative protocol and shared test vectors                                    |
| [internal/core](../../internal/core), [web/core](../../web/core)                                           | Go and TypeScript protocol implementations                                    |
| [internal/pepper](../../internal/pepper)                                                                   | Purpose-separated server keys and pepper rotation                             |
| [internal/store](../../internal/store)                                                                     | SQLite and file-storage adapters and shared storage tests                     |
| [internal/server](../../internal/server)                                                                   | HTTP endpoints, limits, proof of work, headers, cleanup, and embedded client  |
| [cmd/nylzo](../../cmd/nylzo), [internal/cli](../../internal/cli), [internal/client](../../internal/client) | CLI, server commands, and HTTP client                                         |
| [web/app](../../web/app)                                                                                   | Svelte web client with English and Czech interfaces                           |
| [web/e2e](../../web/e2e)                                                                                   | Playwright tests, including CLI/browser interoperability                      |
| [skills/nylzo](../../skills/nylzo)                                                                         | Agent Skills instructions for using the CLI                                   |
| [deploy/compose](../../deploy/compose)                                                                     | Self-hosting example with Caddy and Docker secrets                            |
| [assets/brand](../../assets/brand)                                                                         | Official light/dark logos and brand colors                                    |
| [.github/](../../.github), [.goreleaser.yaml](../../.goreleaser.yaml)                                      | CI, community forms, dependency updates, and release configuration            |
| [docs/](..)                                                                                                | Reviewed documentation sources, operations, governance, designs, and research |

Start with [Development](Development.md) to run the project or [Contributing](../../CONTRIBUTING.md) to submit a change. Earlier research and design documents may be in Czech; current user and contributor guides are in English.
