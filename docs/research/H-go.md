# H: Go for the server and CLI of an encrypted one-time-link tool

Research date: 2026-10-21. Every claim below was checked by opening the page, not from a search snippet. Quotes are copied from the page as fetched; pkg.go.dev pages were rendered at go1.27.3.

Status key: VERIFIED (quote supports the claim), PARTIAL (page supports part of it or the wording differs), NOT FOUND (what was tried is stated).

## 1. Standard library crypto without third-party modules

- Claim: AES block cipher is in the standard library.
  URL: https://pkg.go.dev/crypto/aes
  Quote: "Package aes implements AES encryption (formerly Rijndael)"
  Status: VERIFIED

- Claim: AES-GCM via crypto/cipher; NewGCM exists since Go 1.2 and a random-nonce variant arrived in 1.24.
  URL: https://pkg.go.dev/crypto/cipher
  Quote: NewGCM "added in go1.2"; "NewGCMWithRandomNonce returns the given cipher wrapped in Galois Counter Mode, with randomly-generated nonces." (added in go1.24.0)
  Status: VERIFIED

- Claim: crypto/hkdf was added in Go 1.24 (standard library, RFC 5869, functions Key/Extract/Expand).
  URL: https://go.dev/doc/go1.24 and https://pkg.go.dev/crypto/hkdf
  Quote: "The new crypto/hkdf package implements the HMAC-based Extract-and-Expand key derivation function HKDF, as defined in RFC 5869."
  Status: VERIFIED (the pkg.go.dev page did not print an "added in" tag; the 1.24 release notes carry the version)

- Claim: crypto/pbkdf2 was added in Go 1.24 (standard library, RFC 8018).
  URL: https://go.dev/doc/go1.24 and https://pkg.go.dev/crypto/pbkdf2
  Quote: "The new crypto/pbkdf2 package implements the password-based key derivation function PBKDF2, as defined in RFC 8018." Signature on pkg.go.dev: `func Key[Hash hash.Hash](h func() Hash, password string, salt []byte, iter, keyLength int) ([]byte, error)`
  Status: VERIFIED

- Claim: the new 1.24 packages were promoted from x/crypto.
  URL: https://go.dev/doc/go1.24
  Quote: "All three packages are based on pre-existing golang.org/x/crypto/... packages."
  Status: VERIFIED

- Claim: crypto/sha256 is standard library.
  URL: https://pkg.go.dev/crypto/sha256
  Quote: "Package sha256 implements the SHA224 and SHA256 hash algorithms as defined in FIPS 180-4."
  Status: VERIFIED

- Claim: crypto/subtle.ConstantTimeCompare exists; Go 1.24 added WithDataIndependentTiming.
  URL: https://pkg.go.dev/crypto/subtle
  Quote: "ConstantTimeCompare returns 1 if the two slices, x and y, have equal contents and 0 otherwise."; WithDataIndependentTiming "added in go1.24.0"
  Status: VERIFIED

- Claim: crypto/rand uses the OS CSPRNG, Read cannot fail since 1.24, and rand.Text (1.24) generates random tokens.
  URL: https://pkg.go.dev/crypto/rand and https://go.dev/doc/go1.24
  Quote: "On Linux, FreeBSD, Dragonfly, and Solaris, Reader uses getrandom(2)."; "Read fills b with cryptographically secure random bytes. It never returns an error, and always fills b entirely."; Text "added in go1.24.0", "The result contains at least 128 bits of randomness."
  Status: VERIFIED

- Claim: scrypt and argon2 are not in the standard library; they live in golang.org/x/crypto.
  URL: https://pkg.go.dev/crypto/argon2 (404), https://pkg.go.dev/crypto/scrypt (404), https://pkg.go.dev/golang.org/x/crypto/argon2, https://pkg.go.dev/golang.org/x/crypto/scrypt
  Quote: argon2: "Package argon2 implements the key derivation function Argon2."; scrypt: `func Key(password, salt []byte, N, r, p, keyLen int) ([]byte, error)`; both show module golang.org/x/crypto v0.58.0, published Oct 9, 2026
  Status: VERIFIED (both crypto/argon2 and crypto/scrypt return HTTP 404 on pkg.go.dev)

- Claim: golang.org/x/crypto is a Go project sub-repository, developed through Go's Gerrit and issue tracker.
  URL: https://pkg.go.dev/golang.org/x/crypto and https://pkg.go.dev/golang.org/x/crypto?tab=versions
  Quote: "This repository holds supplementary Go cryptography packages."; "This repository uses Gerrit for code changes."; issues go to go.dev/issues with titles starting "x/crypto:"; git repo go.googlesource.com/crypto; newest tag v0.58.0 (Oct 9, 2026)
  Status: VERIFIED. The Go security policy page (https://go.dev/security/policy) only says "All security bugs in the Go distribution should be reported by email to security@golang.org" and does not name sub-repositories, so coverage of x/crypto under that policy is PARTIAL from the pages opened.

## 2. FIPS 140-3

- Claim: Go 1.24 ships a Go Cryptographic Module selectable with GOFIPS140.
  URL: https://go.dev/doc/security/fips140
  Quote: "The Go Cryptographic Module is a collection of standard library Go packages under crypto/internal/fips140/... that implement FIPS 140-3 approved algorithms."; GOFIPS140 values "off", "latest", "v1.0.0 or v1.26.0", "inprocess and certified"
  Status: VERIFIED

- Claim: module v1.0.0 (Go 1.24+) holds a CMVP certificate.
  URL: https://go.dev/doc/security/fips140
  Quote: "It is covered by CMVP Certificate #5247 and CAVP Certificate A6650."; "It is available in Go 1.24+."
  Status: VERIFIED

- Claim: NIST lists certificate #5247 as an active FIPS 140-3 validation.
  URL: https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/5247
  Quote: Module "Go Cryptographic Module"; vendor "Geomys LLC"; "FIPS 140-3"; overall level "1"; type "Software"; status "Active"; validation dates "4/27/2026" (initial) and "7/7/2026" (update), lab Lightship Security, Inc.; sunset "4/26/2031"
  Status: VERIFIED

- Claim: module v1.26.0 (Go 1.26+) is still in process.
  URL: https://go.dev/doc/security/fips140 and https://csrc.nist.gov/Projects/cryptographic-module-validation-program/modules-in-process/Modules-In-Process-List
  Quote: fips140 page: "As of 2026-04-28, it is Pending Review in the CMVP Modules In Process List." NIST list (Last Updated 10/21/2026): "Go Cryptographic Module" / "Geomys LLC" / "Comment Resolution - CMVP (9/10/2026)"
  Status: VERIFIED for in-process status; PARTIAL on the version, because the NIST list row does not print a version number.

- Claim: FIPS mode needs no code changes for approved algorithms.
  URL: https://go.dev/doc/go1.24
  Quote: "Applications require no changes to use the Go Cryptographic Module for approved algorithms."
  Status: VERIFIED

Note: the certificate vendor is Geomys LLC (Filippo Valsorda's firm), not Google. The fips140 page says older module versions "continue to be supported and available for as long as a more recent version has not obtained a CMVP validation certificate."

## 3. Module supply-chain properties

- Claim: the Go checksum database is a Merkle-tree transparency log run by Google and pins bytes per version.
  URL: https://go.dev/ref/mod (checksum database section) and https://sum.golang.org/
  Quote: "It is a Transparent Log (or “Merkle Tree”) of go.sum line hashes, which is backed by Trillian."; "It also ensures that the bits associated with a specific version do not change from one day to the next, even if the module’s author subsequently alters the tags"; sum.golang.org: "an auditable checksum database which will be used by the go command to authenticate modules."
  Status: VERIFIED

- Claim: go verifies downloads against go.sum and refuses mismatches.
  URL: https://go.dev/ref/mod
  Quote: "If the hash is different from the hash in go.sum, the go command reports a security error and deletes the downloaded file"
  Status: VERIFIED

- Claim: the default proxy is proxy.golang.org and a proxy must serve identical content per version; the mirror does not promise to keep everything forever.
  URL: https://go.dev/ref/mod and https://proxy.golang.org/
  Quote: "The default value of GOPROXY is: https://proxy.golang.org,direct"; "A module proxy must always serve the same content for successful responses for $base/$module/$version.mod and $base/$module/$version.zip"; proxy.golang.org: "does not save all modules forever."; "Whenever possible, the mirror aims to cache content in order to avoid breaking builds"
  Status: VERIFIED. Content is immutable per version; availability is best effort.

- Claim: fetching or building a Go module never runs code from the module (no install scripts).
  URL: https://go.dev/blog/supply-chain (Filippo Valsorda, 31 March 2022)
  Quote: "neither fetching nor building code will let that code execute, even if it is untrusted and malicious."; "Module authors don't register an account and don't upload versions to the proxy."
  Status: VERIFIED

- Claim: govulncheck and vuln.go.dev report only reachable vulnerabilities.
  URL: https://go.dev/doc/security/vuln/ and https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck
  Quote: "only surfaces vulnerabilities that actually affect you, based on which functions in your code are transitively calling vulnerable functions."; govulncheck v1.9.0, published Oct 11, 2026
  Status: VERIFIED

- Claim: the Go toolchain builds reproducibly since 1.21 and is re-verified nightly; -trimpath and CGO_ENABLED=0 make binary builds reproducible.
  URL: https://go.dev/blog/rebuild (Russ Cox, 28 August 2023) and https://pkg.go.dev/cmd/go
  Quote: "Go 1.21.0 is the first Go toolchain with perfectly reproducible builds."; "We run gorebuild nightly, posting the results at go.dev/rebuild"; "Disabling cgo removes the host C toolchain as a relevant input, and -trimpath removes the current directory."; cmd/go: -trimpath "remove all file system paths from the resulting executable."
  Status: VERIFIED

Incidents 2025-2026 (for a fair comparison):

- Claim: a BoltDB typosquat with a backdoor stayed served by the Go Module Mirror for over three years after the GitHub tag was cleaned.
  URL: https://socket.dev/blog/malicious-package-exploits-go-module-proxy-caching-for-persistence (February 4, 2025)
  Quote: module github.com/boltdb-go/bolt; "Once connected, it listens for commands, executes them on the host system, and returns the output to the threat actor."; "Once a module version is cached, it remains accessible through the Go Module Proxy"; uploaded November 2021, "went undetected for over three years."
  Status: VERIFIED

- Claim: three malicious Go modules on GitHub delivered a Linux disk wiper.
  URL: https://socket.dev/blog/wget-to-wipeout-malicious-go-modules-fetch-destructive-payload (May 1, 2025)
  Quote: github[.]com/truthfulpharm/prototransform, github[.]com/blankloggia/go-mcp, github[.]com/steelpoor/tlsproxy; payload would "copy zeros (/dev/zero) directly onto the primary storage device (/dev/sda)"
  Status: VERIFIED

- Claim: CVE-2026-56865 (Go CNA, published 2026-08-13): a malicious GOPROXY could forge sumdb tiles and bypass GOSUMDB.
  URL: https://cveawg.mitre.org/api/cve/CVE-2026-56865
  Quote: "A malicious GOPROXY was previously capable of forging up to two sumdb tiles"; affected cmd/go before 1.25.13, 1.26.0 before 1.26.6, 1.27.0-rc before rc.3; golang.org/x/mod before 0.40.0
  Status: VERIFIED

- Claim: CVE-2026-94447 (Go CNA, published 2026-10-08): a bogus golang.org/toolchain go.sum entry plus a malicious GOMODPROXY could bypass the toolchain checksum.
  URL: https://cveawg.mitre.org/api/cve/CVE-2026-94447
  Quote: "operates a malicious GOMODPROXY the user chooses to use can bypass the intended checksum."; fixed in 1.26.9 and 1.27.2
  Status: VERIFIED. Both 2026 CVEs need a malicious or compromised proxy chosen by the user.

## 4. Official MCP Go SDK

- Claim: github.com/modelcontextprotocol/go-sdk is the official SDK, maintained with Google.
  URL: https://github.com/modelcontextprotocol/go-sdk
  Quote: repository About: "Maintained in collaboration with Google."; "The SDK endeavors to implement the full MCP spec."
  Status: VERIFIED

- Claim: latest release is v1.8.0 (September 2026).
  URL: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk?tab=versions and https://github.com/modelcontextprotocol/go-sdk/releases
  Quote: "v1.8.0 - Sep 4, 2026" (pkg.go.dev tag date); GitHub releases: "v1.8.0", "14 Sep" (year not printed); v1.7.0 Jul 27, 2026
  Status: VERIFIED

- Claim: stdio, Streamable HTTP, SSE and in-memory transports are provided.
  URL: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
  Quote: types StdioTransport, CommandTransport, InMemoryTransport, SSEHandler/SSEClientTransport/SSEServerTransport, "StreamableHTTPHandler, NewStreamableHTTPHandler, StreamableClientTransport, and StreamableServerTransport"
  Status: VERIFIED

- Claim: supported spec revisions include the newest, 2026-07-28.
  URL: https://github.com/modelcontextprotocol/go-sdk
  Quote: latest for SDK v1.7.0+: "2026-07-28"; all supported: "2026-07-28, 2025-11-25*, 2025-06-18, 2025-03-26, 2024-11-05"; footnote: "Partial support for 2025-11-25 (client side OAuth and Sampling with tools not available)."
  Status: VERIFIED

- Claim: Go version policy of the SDK.
  URL: https://github.com/modelcontextprotocol/go-sdk
  Quote: "New releases of the SDK target only supported versions of Go."
  Status: VERIFIED

## 5. Release and support policy

- Claim: the two newest major releases are supported; current is go1.27.3.
  URL: https://go.dev/doc/devel/release
  Quote: "Each major Go release is supported until there are two newer major releases."; "go1.27.3 (released 2026-10-19)"; "go1.26.10 (released 2026-10-19)"; go1.27.0 released 2026-08-19, go1.26.0 released 2026-02-10
  Status: VERIFIED

## 6. Minimal server dependencies

- Claim: pgx v5 current version and Go floor.
  URL: https://pkg.go.dev/github.com/jackc/pgx/v5 and ?tab=versions
  Quote: "v5.11.0: Sep 7, 2026"; "Package pgx is a PostgreSQL database driver."; "pgx supports Go 1.25 and higher."; Imports: 20
  Status: VERIFIED

- Claim: modernc.org/sqlite v1.60.1 is current, cgo-free, SQLite 3.53.4.
  URL: https://pkg.go.dev/modernc.org/sqlite and ?tab=versions
  Quote: "v1.60.1: Sep 29, 2026"; "Package sqlite is a sql/database driver using a CGo-free port of the C SQLite3 library."; SQLite version 3.53.4
  Status: VERIFIED

- Claim: aws-sdk-go-v2 is split into per-service modules; the S3 client alone pulls in ten modules.
  URL: https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/getting-started.html, https://raw.githubusercontent.com/aws/aws-sdk-go-v2/main/service/s3/go.mod, https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3 (+ ?tab=versions, ?tab=imports), https://github.com/aws/aws-sdk-go-v2
  Quote: dev guide: "All API clients are located under github.com/aws/aws-sdk-go-v2/service import hierarchy." with `go get github.com/aws/aws-sdk-go-v2`, `.../config`, `.../service/s3`; service/s3 go.mod requires 10 modules: aws-sdk-go-v2 v1.47.3, aws/protocol/eventstream, internal/configsources, internal/endpoints/v2, internal/v4a, service/internal/accept-encoding, service/internal/checksum, service/internal/presigned-url, service/internal/s3shared, smithy-go v1.28.5; service/s3 v1.114.3 (Oct 9, 2026); Imports: 71 packages (48 non-stdlib); README: "The v2 SDK requires a minimum version of Go 1.24."
  Status: VERIFIED for S3 module weight. Total count of service modules in the repo: NOT FOUND (neither the README nor the dev guide prints a number; the dev guide points to the pkg.go.dev service listing). The dev guide still says "minimum version of Go 1.23" while the README says 1.24.

- Claim: minio-go current version and weight.
  URL: https://pkg.go.dev/github.com/minio/minio-go/v7 and ?tab=versions
  Quote: "v7.4.1: Oct 12, 2026"; "The MinIO Go Client SDK provides straightforward APIs to access any Amazon S3 compatible object storage."; Imports: 73
  Status: VERIFIED

- Claim: hand-rolling SigV4 for PUT/GET/DELETE needs only SHA-256 and HMAC-SHA256, both in the standard library.
  URL: https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_sigv-create-signed-request.html
  Quote: signing key: "DateKey = HMAC-SHA256("AWS4"+"<SecretAccessKey>", "<YYYYMMDD>")" then region, service, "aws4_request"; "The x-amz-content-sha256 header is required for Amazon S3 AWS requests."; "For Amazon S3, include the literal string UNSIGNED-PAYLOAD"; caveat: "Unless you have a good reason not to, we recommend that you always use an SDK or the CLI."; "We recommend that you write your own custom UriEncode function"
  Status: VERIFIED. The algorithm is fully specified on one page; AWS still recommends the SDK.

- Claim: net/http ServeMux has method and wildcard routing since Go 1.22, so no router framework is needed.
  URL: https://go.dev/blog/routing-enhancements (Jonathan Amsterdam, 13 February 2024)
  Quote: "Go 1.22 brings two enhancements to the net/http package's router: method matching and wildcards."; example "GET /posts/{id}"; "The precedence rule is simple: the most specific pattern wins."
  Status: VERIFIED

## 7. Container images and build tooling

- Claim: distroless static is about 2 MiB, Debian 13 based, cosign signed.
  URL: https://github.com/GoogleContainerTools/distroless
  Quote: "The smallest distroless image, gcr.io/distroless/static-debian13, is around 2 MiB."; "That's about 50% of the size of alpine (~5 MiB), and less than 2% of the size of debian (124 MiB)."; "Distroless images are based on Debian 13 (trixie)."; "All distroless images are signed by cosign with ephemeral keys (keyless)."; the Go example uses CGO_ENABLED=0 and `FROM gcr.io/distroless/static-debian13`
  Status: VERIFIED

- Claim: ko builds Go images without Docker, emits SBOMs by default, latest v0.19.1.
  URL: https://ko.build/, https://ko.build/configuration/, https://ko.build/features/sboms/, https://github.com/ko-build/ko/releases
  Quote: "ko is a simple, fast container image builder for Go applications."; "doesn't require docker to be installed."; "From v0.9+, ko generates and uploads an SBOM for every image it produces by default."; "By default, ko bases images on cgr.dev/chainguard/static."; releases: "v0.19.1", "29 Jun" (year not printed; asset timestamps show 2026)
  Status: VERIFIED. Note the default base is Chainguard static, not distroless; set `defaultBaseImage` for distroless. ko signing with cosign or SLSA: NOT FOUND on ko.build (home, configuration, SBOM page, FAQ); signing is done with cosign separately.

- Claim: GoReleaser v2.18.3 signs with cosign (keyless supported) and has a documented SLSA provenance path.
  URL: https://github.com/goreleaser/goreleaser/releases, https://goreleaser.com/customization/sign/, https://goreleaser.com/blog/slsa-generation-for-your-artifacts/
  Quote: "v2.18.3", released "09 Oct" (2026 per asset timestamps); "You can sign your artifacts with cosign as well."; "Cosign uses the --bundle flag, which combines the certificate and signature into a single .sigstore.json file."; blog (October 11, 2023): "generating SLSA provenance for your artifacts with GoReleaser and slsa-github-generator is a straightforward process"
  Status: VERIFIED. GoReleaser's default signer is GnuPG; cosign is configured via `cmd: cosign`.

## 8. Scaleway Serverless Containers

- Claim: any language works as long as the app listens on the PORT env var.
  URL: https://www.scaleway.com/en/serverless-containers/
  Quote: "you can rapidly deploy your containers in any language you want."; "You can run any containerized web application as long as it listens on the PORT environment variable."
  Status: VERIFIED

- Claim: platform requirements: PORT env var, HTTP/1.1 or HTTP/2, linux/amd64, image under 1 GB recommended, request timeout up to 60 min.
  URL: https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-limitations/
  Quote: "PORT: Value of the port defined in the Container settings, which the container has to listen on."; "Serverless Containers support HTTP/1.1 and HTTP/2 connections. HTTP/1.0 is not supported."; "Image target platform must be: linux/amd64."; "We recommend keeping your image size below 1 GB"
  Status: VERIFIED

- Claim: port 8080 is the default.
  URL: https://www.scaleway.com/en/docs/serverless-containers/reference-content/port-parameter-variable.md
  Quote: "We therefore recommend you use the $PORT variable in your application, as it will contain the port parameter value"; "must be the same as the port exposed by your containerized application."; 8080 appears only as `const port = process.env.PORT || 8080;` in the example
  Status: PARTIAL. Scaleway's own docs opened here do not state 8080 as the default; read PORT at runtime and fall back to 8080.

## 9. Rust comparison (brief)

- RustCrypto aes-gcm 0.11.1 had one NCC Group audit (report dated 26 February 2020, commissioned by MobileCoin, "Two consultants provided five person-days of effort."). docs.rs: "This crate has received one security audit by NCC Group, with no significant findings." and "It is not suitable for use on processors with a variable-time multiplication operation". URLs: https://docs.rs/aes-gcm/latest/aes_gcm/ and https://www.nccgroup.com/research/public-report-rustcrypto-aesgcm-and-chacha20pluspoly1305-implementation-review/ . VERIFIED. RustCrypto AEADs MSRV is 1.85 (https://github.com/RustCrypto/AEADs).
- ring is at 0.17.14 (crates.io updated 2025-03-11), self-described as "An experiment."; "Most of the C and assembly language code in ring comes from BoringSSL."; no audit or FIPS statement on the README. URLs: https://crates.io/api/v1/crates/ring and https://github.com/briansmith/ring . VERIFIED.
- aws-lc-rs "strives to be API-compatible with the popular Rust library named ring" and its FIPS crate "provides bindings to AWS-LC-FIPS 3.x, which has completed FIPS validation testing by an accredited lab and has been submitted to NIST for certification." URL: https://github.com/aws/aws-lc-rs . VERIFIED (no certificate number on that page).
- Rust's std has no cryptography: the std module index lists no crypto, cipher or aes module; `hash` is "Generic hashing support" (trait machinery) and `random` is experimental only. URL: https://doc.rust-lang.org/std/ . VERIFIED.
- Net: Rust needs third-party crates for every primitive used here (AES-GCM, HKDF, PBKDF2/argon2, CSPRNG wrapper), whereas Go 1.24+ covers all but argon2/scrypt in the standard library, with a CMVP-certified module option.

## Decision-relevant summary

- Go 1.24+ standard library covers AES-GCM, HKDF, PBKDF2, SHA-256, constant-time compare and a non-failing CSPRNG with zero third-party modules; only argon2/scrypt need golang.org/x/crypto, which is a Go project sub-repository.
- The Go Cryptographic Module v1.0.0 (Go 1.24+) holds active CMVP certificate #5247 (FIPS 140-3, Level 1, validated 4/27/2026, sunset 4/26/2031, vendor Geomys LLC); v1.26.0 is in CMVP comment resolution as of 9/10/2026. GOFIPS140=v1.0.0 or `certified` selects it with no code changes.
- Supply chain: modules execute nothing at fetch or build time; sum.golang.org pins bytes per version; govulncheck reports only reachable vulnerabilities; the toolchain is reproducible and re-verified nightly, and CGO_ENABLED=0 plus -trimpath makes your binary reproducible.
- Counterweights: the proxy served a backdoored BoltDB typosquat for three years (Socket, Feb 2025), three wiper modules appeared on GitHub (May 2025), and two 2026 toolchain CVEs (56865, 94447) let a malicious proxy bypass checksum checks; both need a user-chosen rogue proxy and are fixed in 1.26.9/1.27.2 and later. Keep GOPROXY at the default and stay on a current patch release.
- Support: two newest majors are supported; today that is go1.27.3 and go1.26.10 (both 2026-10-19).
- MCP: github.com/modelcontextprotocol/go-sdk v1.8.0 (Sep 2026) is official, maintained with Google, ships stdio and Streamable HTTP, and negotiates spec 2026-07-28.
- Server deps can be tiny: net/http with Go 1.22 pattern routing, pgx v5.11.0 (Go 1.25+) or modernc.org/sqlite v1.60.1 (cgo-free). For S3, aws-sdk-go-v2/service/s3 v1.114.3 drags in ten modules; minio-go v7.4.1 is one module with 73 imports; a hand-rolled SigV4 signer for PUT/GET/DELETE needs only HMAC-SHA256 and a careful UriEncode, but AWS recommends the SDK.
- Packaging: static Go binary on gcr.io/distroless/static-debian13 (~2 MiB, cosign-signed base); build with ko (SBOM by default, no Docker) or GoReleaser v2.18.3 (cosign keyless, SLSA via slsa-github-generator).
- Scaleway Serverless Containers runs any language; the binary must listen on $PORT over HTTP/1.1 or HTTP/2 and target linux/amd64. Treat 8080 as a fallback, not a documented default.
- Rust alternative: RustCrypto aes-gcm has one 2020 NCC audit with no significant findings; ring calls itself an experiment; aws-lc-rs offers a FIPS path via AWS-LC, but Rust's std has no crypto at all, so every primitive is a third-party crate.

## URLs opened

- https://go.dev/doc/go1.24
- https://pkg.go.dev/crypto/aes
- https://pkg.go.dev/crypto/cipher
- https://pkg.go.dev/crypto/hkdf
- https://pkg.go.dev/crypto/pbkdf2
- https://pkg.go.dev/crypto/sha256
- https://pkg.go.dev/crypto/subtle
- https://pkg.go.dev/crypto/rand
- https://pkg.go.dev/crypto/argon2 (404)
- https://pkg.go.dev/crypto/scrypt (404)
- https://pkg.go.dev/golang.org/x/crypto
- https://pkg.go.dev/golang.org/x/crypto?tab=versions
- https://pkg.go.dev/golang.org/x/crypto/argon2
- https://pkg.go.dev/golang.org/x/crypto/scrypt
- https://go.dev/security/policy
- https://go.dev/doc/security/fips140
- https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/5247
- https://csrc.nist.gov/Projects/cryptographic-module-validation-program/modules-in-process/Modules-In-Process-List
- https://go.dev/ref/mod
- https://sum.golang.org/
- https://proxy.golang.org/
- https://go.dev/blog/supply-chain
- https://go.dev/blog/rebuild
- https://pkg.go.dev/cmd/go
- https://go.dev/doc/security/vuln/
- https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck
- https://socket.dev/blog/malicious-package-exploits-go-module-proxy-caching-for-persistence
- https://socket.dev/blog/wget-to-wipeout-malicious-go-modules-fetch-destructive-payload
- https://cveawg.mitre.org/api/cve/CVE-2026-56865
- https://cveawg.mitre.org/api/cve/CVE-2026-94447
- https://github.com/modelcontextprotocol/go-sdk
- https://github.com/modelcontextprotocol/go-sdk/releases
- https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
- https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk?tab=versions
- https://go.dev/doc/devel/release
- https://pkg.go.dev/github.com/jackc/pgx/v5
- https://pkg.go.dev/github.com/jackc/pgx/v5?tab=versions
- https://pkg.go.dev/modernc.org/sqlite
- https://pkg.go.dev/modernc.org/sqlite?tab=versions
- https://github.com/aws/aws-sdk-go-v2
- https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/getting-started.html
- https://raw.githubusercontent.com/aws/aws-sdk-go-v2/main/service/s3/go.mod
- https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3
- https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3?tab=versions
- https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3?tab=imports
- https://pkg.go.dev/github.com/minio/minio-go/v7
- https://pkg.go.dev/github.com/minio/minio-go/v7?tab=versions
- https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_sigv-create-signed-request.html
- https://go.dev/blog/routing-enhancements
- https://github.com/GoogleContainerTools/distroless
- https://ko.build/
- https://ko.build/configuration/
- https://ko.build/features/sboms/
- https://ko.build/advanced/faq/
- https://github.com/ko-build/ko/releases
- https://github.com/goreleaser/goreleaser/releases
- https://goreleaser.com/customization/sign/
- https://goreleaser.com/blog/slsa-generation-for-your-artifacts/
- https://www.scaleway.com/en/serverless-containers/
- https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-limitations/
- https://www.scaleway.com/en/docs/serverless-containers/reference-content/port-parameter-variable.md
- https://docs.rs/aes-gcm/latest/aes_gcm/
- https://www.nccgroup.com/research/public-report-rustcrypto-aesgcm-and-chacha20pluspoly1305-implementation-review/
- https://github.com/RustCrypto/AEADs
- https://github.com/briansmith/ring
- https://crates.io/api/v1/crates/ring
- https://github.com/aws/aws-lc-rs
- https://doc.rust-lang.org/std/

Pages tried that returned nothing usable: https://aws.github.io/aws-sdk-go-v2/docs/getting-started/ (404), https://www.scaleway.com/en/docs/serverless-containers/reference-content/containers-concepts/ (404), https://www.scaleway.com/en/docs/serverless-containers/concepts/ and .../quickstart/ (body not rendered), https://ko.build/features/ (404), https://crates.io/crates/ring (JS-only; used the API instead).
