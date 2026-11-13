# Self-hosting

Deployment profiles live under [`deploy/`](../../../deploy). The Compose example below is the existing runtime reference. The [Hetzner/Flatcar profile](Hetzner-Flatcar.md) adds OpenTofu provisioning, verified updates, and optional Cloudflare DNS-only/DNSSEC; it remains experimental until real-cloud acceptance is complete.

The [Compose example](../../../deploy/compose) runs Nylzo behind Caddy with TLS. Use a Linux server with Docker Engine 28.0 or newer, Docker Compose v2, a domain pointing to the server, and ports 80/tcp, 443/tcp, and 443/udp open.

Caddy comes from the same release as Nylzo and embeds the same static web client. It serves pages, assets and the client manifest itself; only API requests, the transparency JSON and health checks reach the backend. The release workflow builds `ghcr.io/misaon/nylzo-caddy` from [deploy/caddy](../../../deploy/caddy) with the repository's pinned Go toolchain and dependency versions, so a Go or dependency fix does not wait for an upstream image rebuild.

The TLS endpoint sees ciphertext, request signatures, and leases, and can replace the web client. Terminate TLS on infrastructure you trust with the server itself. Read the [security model](Security-model.md) and [operations guide](Operations.md) before deployment.

## Prepare the deployment

1. Download and [verify a release](Verify-a-release.md). Verify the signatures and provenance of both container images against the selected tag and pin their digests.
2. Copy [compose.yaml](../../../deploy/compose/compose.yaml), [Caddyfile](../../../deploy/compose/Caddyfile), [check-isolation](../../../deploy/compose/check-isolation), and [.env.example](../../../deploy/compose/.env.example) into a private deployment directory. Copy `.env.example` to `.env`.
3. Set `NYLZO_IMAGE` and `NYLZO_CADDY_IMAGE` to the digest-pinned references printed by the verification guide. Set `NYLZO_DOMAIN` to your lowercase domain and `NYLZO_GLOBAL_DAILY_CREATES` to a cap your disk can support.
4. Fill in `NYLZO_OPERATOR`, `NYLZO_CONTACT` (email or HTTPS address), and `NYLZO_HOSTING`. They appear on `/transparency`. Compose derives `NYLZO_PUBLIC_URL` from the domain for canonical URLs, previews, and the sitemap.
5. Keep Docker's data root, which holds the `nylzo-data` volume, on ext4 or XFS: Nylzo erases a deleted message by overwriting its blocks in place and refuses to start on btrfs, ZFS or an overlay. The Compose file raises the memlock limit, because the server locks its memory against swap.

Generate a server pepper in the deployment directory. The container runs as UID 65532 and must be the only account able to read the file:

```sh
umask 077
mkdir -p secrets
openssl rand -hex 32 > secrets/pepper
sudo chown 65532:65532 secrets/pepper
sudo chmod 0400 secrets/pepper
```

Create Caddy's state directory for its certificates and ACME account. Caddy runs as UID 65533, separate from Nylzo's 65532, so its account owns neither the pepper nor Nylzo's data, even outside the container:

```sh
sudo install -d -o 65533 -g 65533 -m 0700 caddy-state
```

Neither UID may belong to an account on the host. Compose refuses to start when `caddy-state` is missing, rather than creating a directory Caddy cannot write.

Run the pepper commands only for a new deployment; do not overwrite an existing pepper during an upgrade. Keep it outside the database. Losing the pepper makes existing tokens unverifiable and live messages inaccessible. A leaked pepper combined with server data enables additional guessing attacks, and together with a public key from a recorded request it opens that message's stored metadata; see the [protocol](../reference/Protocol.md).

On your own workstation, create the abuse-reporting key with a verified CLI:

```sh
nylzo report-key --key-file "$HOME/nylzo-report-key"
```

Copy the printed **public** `NYLZO_REPORT_KEY=…` value into `.env`. Keep the private key on the workstation and off the server; it is needed to decrypt reports. Encrypt it right away, for example `age --encrypt --passphrase --output "$HOME/nylzo-report-key.age" "$HOME/nylzo-report-key" && rm "$HOME/nylzo-report-key"`, and decrypt it only into a pipe when you read reports.

Start from the deployment directory:

```sh
docker compose up -d
```

Then check that Nylzo's network is isolated:

```sh
sudo bash check-isolation
```

The script fails when:

- Docker Engine is older than 28.0. Engine 26 and older accept the `isolated` gateway mode without applying it, which leaves services on the host reachable from Nylzo.
- The host has an address on the `backend` network, or has reverse-path filtering off for it and would accept broadcasts from Nylzo. Set `net.ipv4.conf.all.rp_filter = 2` in `/etc/sysctl.d/` to turn the filter on.
- A container other than Nylzo and Caddy is attached to `backend`, or Nylzo is attached to another network or has a default route.
- The Caddyfile proxies to an address other than Nylzo's.

Run the check again after every Docker or Nylzo upgrade.

Check the HTTPS instance and its `/transparency` page. Run `nylzo verify-client --server https://your-domain.example` with a `nylzo` of the release the instance runs. [Homebrew and Scoop](CLI.md#install) install the latest release, the only one that receives security fixes. Understand the [limits of that check](Security-model.md#the-client-is-part-of-the-trust-boundary).

## What the example configures

- Nylzo has a read-only root filesystem, drops all Linux capabilities, and sets `no-new-privileges`. Only `nylzo-data` is writable. The pepper is a Docker secret file, rather than a process environment value. It may use 512 MiB, 1.5 CPUs and 128 processes, and has no memory-lock limit (`memlock: -1`), so it can lock its memory and keep secrets out of swap.
- Caddy has the same restrictions and runs as UID and GID 65533, apart from Nylzo's 65532, with 512 MiB, one CPU and 256 processes. Its container sets `net.ipv4.ip_unprivileged_port_start` to 80, which lets it bind ports 80 and 443 without `NET_BIND_SERVICE`. Only the `caddy-state` directory, which holds certificates and the ACME account, is writable. Its admin API is off. Neither container writes core dumps.
- Caddy accepts only TLS 1.3 with X25519MLKEM768, the hybrid post-quantum key exchange every browser in `web/.browserslistrc` uses by default. Older clients cannot connect, among them Safari and iOS before 26, Firefox ESR 128, curl on Ubuntu 24.04, and many link-preview bots and uptime monitors. Only the published Google, Bing and Seznam crawler addresses may also use X25519, and they reach only the public pages ([transport](Security-model.md#transport)). Caddy runs with `--adapter nylzo`, which adds that exception; started any other way it refuses the Caddyfile.
- Caddy limits headers to 16 KB and bodies to 6 MiB, and closes connections that stall: 10 s for headers, 20 s without body data, 6 minutes for a whole body or response, 2 minutes idle. When Caddy itself fails a request, for example because Nylzo does not answer, a browser that navigated gets the server error page from the frontend under that page's CSP, and every other request gets the bare status with a `default-src 'none'` CSP. Both carry `nosniff`, HSTS and `no-store`.
- Nylzo is attached only to `backend`, an internal network with IPv4 and IPv6. Nylzo has no route out, the host has no address on `backend` (gateway mode `isolated`), and Docker forwards none of Nylzo's DNS lookups, so it resolves only container names. A compromised Nylzo can open connections to Caddy and to nothing outside `backend`. Caddy is also attached to `edge`, where Docker publishes its ports and through which it reaches Let's Encrypt. On `backend` Caddy uses fixed addresses `172.30.82.2` and `fd1f:9791:e50b:1::2`, and Nylzo trusts only those proxy addresses. Caddy connects to Nylzo's fixed address `172.30.82.3` instead of its name: while the Nylzo container is down, Docker would pass a lookup of `nylzo` to the host's resolvers. Caddy sets `X-Forwarded-For` itself and removes client-sent `X-Real-IP`, `Forwarded`, `True-Client-IP` and similar headers. Other containers use the upper half of each `backend` subnet. If these ranges conflict with your network, change the subnets, the Caddy and Nylzo addresses, the Caddyfile upstream and the trusted proxies together. Docker picks the `edge` subnets from its own address pools; give `edge` an `ipam` block to pin them.
- Docker Engine's default IPv6 firewall support is required. Without IPv6 on the `edge` network, Docker's userland proxy can make all IPv6 users share one source-address rate-limit bucket. Docker Desktop also proxies incoming traffic and can collapse source addresses; use Linux with Docker Engine for production.
- The global daily cap defaults to 2,000 creations. Proof of work starts at 1,000; at the cap, creation returns 503 while reading continues. Address limits reset with the process, so the global cap protects disk capacity against clients rotating addresses.
- A maximum-size message is about 5.4 MB and may live for seven days. Budget up to eight daily caps of stored messages: about 86 GB at 2,000 creations per day. Adjust the cap for your disk. Below the 1 GiB free-space reserve, creation stops while reading, cancellation, and deletion continue.
- Both containers use the `local` log driver with at most three 10 MB files. Caddy's access logging and error loggers containing request details are disabled, preventing proxy failures from logging addresses, message paths, or `X-Nylzo-*` headers. Startup and certificate logs remain. Keep `http.log`, `http.handlers` and `http.stdlib` excluded and add no `log` directive to a site: a `log` block with any setting, `format` or `level` included, becomes an access log of its own that the exclusions do not reach. Caddy redacts `Authorization` but not `X-Nylzo-*`, and a logged `X-Nylzo-Lease` lets anyone with the link download a message without spending an open. The `nylzo` adapter refuses a Caddyfile that does any of this or uses `tracing`, the FastCGI transport or `insecure_secrets_log`, and the frontend does not start from a Caddyfile loaded without that adapter. For the same reason Nylzo and its Caddy refuse to start with `GODEBUG=http2debug=1` or `=2`, and Caddy also with `QLOGDIR`, which makes it write HTTP/3 headers to files.
- Caddy reuses idle upstream connections for at most 25 seconds, below Nylzo's 30-second idle timeout. It removes `Server` and `Via` headers, including on errors and HTTP redirects, and closes requests for other hosts without an HTTP response. Caddy supplies the frontend's CSP and security headers. Backend responses pass through a data-only boundary: only JSON, encrypted blob bytes and plain-text health responses are accepted; redirects, unexpected content types and encoded bodies are rejected. Rejected upstream replies are closed before their bodies are read, and successful bodyless acknowledgements remain HTTP 204. Only content length, retry timing and the three protocol headers are forwarded. Cookies, refreshes, resource hints, service-worker permissions and backend security policies are discarded.

The frontend is part of Caddy's read-only executable, outside the backend process and its writable data volume. Compromising the backend alone cannot replace frontend pages or assets through this deployment. The host, Caddy, the release process and browser/device security remain trusted; a client-side injection vulnerability would still require its own fix.

## Process abuse reports

The server stores reports sealed to the operator's public key in daily UTC files under `abuse/`. The image has no shell. Copy reports out to your workstation; never `docker cp` or `docker compose cp` anything into the Nylzo container, because Docker 28 and older let a compromised container redirect such a copy onto host files (GHSA-vp62-88p7-qqf5, GHSA-x86f-5xw2-fm2r):

```sh
docker compose cp nylzo:/home/nonroot/abuse/. ./abuse
```

Decrypt them on the workstation with the private reporting key:

```sh
cat abuse/*.sealed | nylzo reports --key-file <(age --decrypt "$HOME/nylzo-report-key.age")
```

After review, place approved message IDs in a private file, one per line. Run from the deployment directory:

```sh
docker compose exec -T nylzo /ko-app/nylzo takedown < approved-ids.txt
```

Run commands in the running container with `docker compose exec`. A `docker compose run nylzo` container takes Nylzo's fixed address, so it fails while Nylzo runs and receives Caddy's API requests while Nylzo is stopped.

Takedown deletes the server's message blob without reading its content. Use an optional reporter contact only for acknowledgement and the moderation decision. Delete decrypted reports after review; they contain message IDs and contact addresses. See [Operations](Operations.md#abuse-reports-and-takedown) for retention, quotas, and sanitization.

## Maintenance

Create an empty file `caddy-state/maintenance` to put the instance into maintenance, and delete it to end maintenance. Caddy checks for the file on every request, so neither step needs a restart. While the file exists:

- a page request gets the maintenance page in English or Czech, chosen by `Accept-Language`, with status 503 and `Retry-After: 30`. The page reloads itself every 30 seconds and runs no application code.
- API and transparency requests get 503 `{"error":"unavailable"}` with `Retry-After: 30` and never reach Nylzo.
- Assets and `/healthz` pass as usual, so the page loads its styles and monitoring still sees Nylzo.

Messages keep expiring on schedule, because expiry follows `expires_at`.

## Upgrade and rotate keys

Keep both images, the Caddyfile, `compose.yaml` and `check-isolation` from the same release: the `nylzo_frontend` directive and the `nylzo_crawler_tls` option need Nylzo's Caddy build, and `compose.yaml` starts it with `--adapter nylzo`. An older or upstream-only Caddy rejects either, and the plain Caddyfile adapter refuses the option. If you serve another host name from the same site block, such as `www`, name it in `nylzo_crawler_tls` too, or the adapter refuses the Caddyfile. The `contact` lines of `nylzo_frontend` and `nylzo_error` take `NYLZO_CONTACT` too, so the pages Caddy serves may link the operator's contact; give the Caddy container the same value as the server. Verify both new images against their exact tag, update the digest-pinned `NYLZO_IMAGE` and `NYLZO_CADDY_IMAGE`, run `docker compose up -d` and then `sudo bash check-isolation`. Do not overwrite the pepper or reporting key. Read the [operations guide](Operations.md#logging-and-upgrades) for schema compatibility.

A deployment that still runs the official `caddy` image must also take the new `compose.yaml` and create `caddy-state` as shown above. Caddy then runs as UID 65533, keeps its state in that directory and requests a new certificate on first start. Once HTTPS works, remove the old volumes with `docker volume rm nylzo_caddy-data nylzo_caddy-config`.

A deployment whose `compose.yaml` still has a single `nylzo` network moves to `backend` and `edge` with the same `docker compose up -d`, which recreates both containers and needs Docker Engine 28.0 or newer. The old network stays behind without containers, and Compose no longer manages it: delete it with `docker network rm nylzo_nylzo`. To keep subnets you chose for it, run `docker compose down` before you replace `compose.yaml`, because Docker refuses a new network whose ranges an existing one holds; `down` keeps the `nylzo-data` volume. Host scripts that reached Nylzo at its container address stop working, since the host has no address on `backend`.

The Caddyfile requests ordinary 90-day certificates. To match the Hetzner profile, add `issuer acme { profile shortlived }` to the site's `tls` block for 160-hour certificates, and alert on certificate expiry. Publish CAA `issue "letsencrypt.org; validationmethods=http-01,tls-alpn-01; accounturi=…"` and `issuewild ";"` for your domain, and turn off any CDN-managed certificates for it.

For pepper rotation, keep the previous pepper as a second Docker secret and configure `NYLZO_PEPPER_PREVIOUS_FILE`, then generate the new pepper. The first start with both seals every stored message with the new pepper. Retain the previous pepper for seven days so passphrase checks and leases of live messages still verify. See [Operations](Operations.md#configuration).

The example's retention policy does not back up message content. Keep configuration and pepper backups separate from message data, and keep the reporting private key on the workstation. Review your own retention policy before adding database, volume, or host-level backups. Such a backup must leave out the epoch seed (`epoch/` in the data volume): a copy of it, together with a link and a copy of the message, opens at any later time every message that was live when the backup was taken or whose id the server issued in the rest of that hour. The same holds for a disk image or provider snapshot of the host. To keep the seed off the disk, bind-mount a host directory on tmpfs, such as `/run/nylzo-epoch` owned by UID 65532 with mode 0700, and set `NYLZO_EPOCH_DIR` to it; a host reboot then ends every live message. See [Expiry keys](Operations.md#expiry-keys).

## Choosing a runtime for a new VPS

For a dedicated Nylzo VPS, rootless Podman managed by systemd Quadlet is a reasonable alternative to the tested Compose example. Caddy can run as a host service and proxy to Nylzo's loopback-only published port. This keeps application containers under a dedicated unprivileged account and uses the host's service manager for startup, restart policy, and resource limits.

Rootless Quadlet requires the user's unit search path and a working user service manager, normally with lingering enabled for the dedicated service account. Adding `User=` or `DynamicUser=` to a rootful Quadlet does not make the container rootless. Use a supported Podman release and cgroup v2. Keep the default seccomp profile and the distribution's AppArmor or SELinux policy enabled.

Carry over the Compose protections: a digest-pinned verified image, read-only root filesystem, no capabilities, no new privileges, explicit memory/process limits, one writable data volume, a separately managed pepper secret, and a network that gives Nylzo no route out, such as a Podman `--internal` network. Publishing a port does not limit where Nylzo can connect. Rootless UID mappings differ from host UIDs; do not copy the Compose `chown 65532` command to a rootless deployment. Confirm the observed proxy address and forwarding-header behavior before configuring the exact trusted proxy. Never expose Nylzo's backend port publicly.

Expose only HTTPS and the ports needed for certificate issuance. Restrict administrative SSH to a VPN or an explicit firewall allowlist, use a dedicated administrator account and strong SSH authentication, and disable password/root SSH login after testing access. Apply supported OS security updates, monitor disk space and health, and keep encrypted backups off the VPS. Test restoration of both application data and the pepper. A live SQLite database needs a consistent backup procedure; copying only its database file can lose WAL state.

A pull-based deployment agent can check for a new image digest, verify its Cosign signature and GitHub provenance against a published immutable release, and update the Quadlet's pinned image reference. Give GitHub no root or SSH credential for the VPS. Check health and reported version after a restart, and define rollback rules around database compatibility and a tested backup. The Nylzo repository does not yet provide a tested Quadlet deployment or production updater.

Coolify offers a convenient deployment dashboard, but it also adds a control plane and privileged server access to maintain. Its own security documentation requires separate host hardening, patching, and workload backups. For one Nylzo instance, choose a dashboard only if its operational convenience justifies that additional component.

- [Podman: Quadlet and rootless units](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
- [Caddy: automatic HTTPS](https://caddyserver.com/docs/automatic-https)
- [Coolify: security model](https://coolify.io/docs/core/security-model)
