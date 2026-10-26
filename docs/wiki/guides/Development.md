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

`test` runs Go with the race detector and the TypeScript suites. `lint` runs Go, TypeScript, workflow, and shell checks. CI also runs fuzz smoke tests, Compose integration, dependency review, three browser engines, and reproducible builds. `mise tasks` lists all tasks.

`mise run build` embeds the production web client in `bin/nylzo` and injects the same build identity into Go and Vite. Untagged and modified checkouts identify themselves as development builds. See [versioning](../maintainers/Releasing.md#one-source-of-version-identity).

## Protocol vectors and memory

After a protocol-core change, regenerate the shared vectors:

```sh
go run ./cmd/nylzo-vectors -out spec/vectors.json
```

The Go generator and TypeScript implementation must agree with the committed vectors. The largest RFC 7914 scrypt vector needs 1 GiB of memory. TypeScript runs it with `NYLZO_HEAVY_TESTS=1`, enabled in CI. `go test -short` skips its recalculation in the core; the vector generator still computes it.

The web app and ESLint use the TypeScript version in [web/package.json](../../../web/package.json) for tool compatibility; the core and browser tests pin theirs separately. Use the checked-in package manager and lockfile rather than updating tools implicitly.

## Section links

Home-page section links use English fragments in both languages, such as `#create`, `#how-it-works`, and `#security`. Earlier Czech fragments remain as invisible anchors so shared links keep working, including before JavaScript loads. Message-link fragments keep their protocol-defined format.
