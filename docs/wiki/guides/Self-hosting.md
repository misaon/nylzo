# Self-hosting

Deployment profiles live under [`deploy/`](../../../deploy). The Compose example below is the existing runtime reference. The [Hetzner/Flatcar profile](Hetzner-Flatcar.md) adds OpenTofu provisioning, verified updates, and optional Cloudflare DNS-only/DNSSEC; it remains experimental until real-cloud acceptance is complete.

The [Compose example](../../../deploy/compose) runs Nylzo behind Caddy with TLS. Use a Linux server with Docker Engine, Docker Compose v2, a domain pointing to the server, and ports 80/tcp, 443/tcp, and 443/udp open.

Caddy comes from the same release as Nylzo. The release workflow builds `ghcr.io/misaon/nylzo-caddy` from [deploy/caddy](../../../deploy/caddy) with the repository's pinned Go toolchain and dependency versions, so a Go or dependency fix does not wait for an upstream image rebuild.

The TLS endpoint sees ciphertext, request signatures, and leases, and can replace the web client. Terminate TLS on infrastructure you trust with the server itself. Read the [security model](Security-model.md) and [operations guide](Operations.md) before deployment.

## Prepare the deployment

1. Download and [verify a release](Verify-a-release.md). Verify the signatures and provenance of both container images against the selected tag and pin their digests.
2. Copy [compose.yaml](../../../deploy/compose/compose.yaml), [Caddyfile](../../../deploy/compose/Caddyfile), and [.env.example](../../../deploy/compose/.env.example) into a private deployment directory. Copy `.env.example` to `.env`.
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

Run the pepper commands only for a new deployment; do not overwrite an existing pepper during an upgrade. Keep it outside the database. Losing the pepper makes existing tokens unverifiable and live messages inaccessible. A leaked pepper combined with server data enables additional guessing attacks; see the [protocol](../reference/Protocol.md).

On your own workstation, create the abuse-reporting key with a verified CLI:

```sh
nylzo report-key --key-file "$HOME/nylzo-report-key"
```

Copy the printed **public** `NYLZO_REPORT_KEY=…` value into `.env`. Keep the private key on the workstation and off the server; it is needed to decrypt reports. Encrypt it right away, for example `age --encrypt --passphrase --output "$HOME/nylzo-report-key.age" "$HOME/nylzo-report-key" && rm "$HOME/nylzo-report-key"`, and decrypt it only into a pipe when you read reports.

Start from the deployment directory:

```sh
docker compose up -d
```

Check the HTTPS instance and its `/transparency` page. Run `nylzo verify-client --server https://your-domain.example` with the matching verified release; understand the [limits of that check](Security-model.md#the-client-is-part-of-the-trust-boundary).

## What the example configures

- Nylzo has a read-only root filesystem, drops all Linux capabilities, and sets `no-new-privileges`. Only `nylzo-data` is writable. The pepper is a Docker secret file, rather than a process environment value. It may use 512 MiB, 1.5 CPUs and 128 processes, and may lock all of its memory (`memlock`) so secrets stay out of swap.
- Caddy has the same restrictions and runs as UID and GID 65533, apart from Nylzo's 65532, with 512 MiB, one CPU and 256 processes. Its container sets `net.ipv4.ip_unprivileged_port_start` to 80, which lets it bind ports 80 and 443 without `NET_BIND_SERVICE`. Only the `caddy-state` directory, which holds certificates and the ACME account, is writable. Its admin API is off. Neither container writes core dumps.
- Caddy accepts only TLS 1.3 with X25519MLKEM768, the hybrid post-quantum key exchange every browser in `web/.browserslistrc` uses by default. Clients built on OpenSSL older than 3.5, such as curl on Ubuntu 24.04, cannot connect. HTTP/3 0-RTT is off, so replayed early data never reaches Nylzo and every request comes from a validated client address. Unknown host names get no certificate (`strict_sni_host`) and only Let's Encrypt issues the certificate.
- Caddy limits headers to 16 KB and bodies to 6 MiB, and closes connections that stall: 10 s for headers, 20 s without body data, 6 minutes for a whole body or response, 2 minutes idle. Its own error pages carry a `default-src 'none'` CSP, `nosniff`, HSTS and `no-store`.
- The network has IPv4 and IPv6. Caddy uses fixed addresses `172.30.80.2` and `fd1f:9791:e50b::2`. Nylzo trusts only those proxy addresses. Caddy sets `X-Forwarded-For` itself and removes client-sent `X-Real-IP`, `Forwarded`, `True-Client-IP` and similar headers. Other containers use the upper half of each subnet. If these ranges conflict with your network, change the subnets, Caddy addresses, and trusted proxies together.
- Docker Engine's default IPv6 firewall support is required. Without the IPv6 network, Docker's userland proxy can make all IPv6 users share one source-address rate-limit bucket. Docker Desktop also proxies incoming traffic and can collapse source addresses; use Linux with Docker Engine for production.
- The global daily cap defaults to 2,000 creations. Proof of work starts at 1,000; at the cap, creation returns 503 while reading continues. Address limits reset with the process, so the global cap protects disk capacity against clients rotating addresses.
- A maximum-size message is about 5.4 MB and may live for seven days. Budget up to eight daily caps of stored messages: about 86 GB at 2,000 creations per day. Adjust the cap for your disk. Below the 1 GiB free-space reserve, creation stops while reading, cancellation, and deletion continue.
- Both containers use the `local` log driver with at most three 10 MB files. Caddy's access logging and error loggers containing request details are disabled, preventing proxy failures from logging addresses, message paths, or `X-Nylzo-*` headers. Startup and certificate logs remain.
- Caddy reuses idle upstream connections for at most 25 seconds, below Nylzo's 30-second idle timeout. It removes `Server` and `Via` headers, including on errors and HTTP redirects, and closes requests for other hosts without an HTTP response. Nylzo supplies CSP and the remaining security headers.

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

Takedown deletes the server's message blob without reading its content. Use an optional reporter contact only for acknowledgement and the moderation decision. Delete decrypted reports after review; they contain message IDs and contact addresses. See [Operations](Operations.md#abuse-reports-and-takedown) for retention, quotas, and sanitization.

## Upgrade and rotate keys

Verify both new images against their exact tag, update the digest-pinned `NYLZO_IMAGE` and `NYLZO_CADDY_IMAGE`, and run `docker compose up -d`. Do not overwrite the pepper or reporting key. Read the [operations guide](Operations.md#logging-and-upgrades) for schema compatibility.

A deployment that still runs the official `caddy` image must also take the new `compose.yaml` and create `caddy-state` as shown above. Caddy then runs as UID 65533, keeps its state in that directory and requests a new certificate on first start. Once HTTPS works, remove the old volumes with `docker volume rm nylzo_caddy-data nylzo_caddy-config`.

The Caddyfile requests ordinary 90-day certificates. To match the Hetzner profile, add `issuer acme { profile shortlived }` to the site's `tls` block for 160-hour certificates, and alert on certificate expiry. Publish CAA `issue "letsencrypt.org; validationmethods=http-01,tls-alpn-01; accounturi=…"` and `issuewild ";"` for your domain, and turn off any CDN-managed certificates for it.

For pepper rotation, keep the previous pepper as a second Docker secret and configure `NYLZO_PEPPER_PREVIOUS_FILE`, then generate the new pepper. Retain the previous pepper for seven days so live messages remain accessible. See [Operations](Operations.md#configuration).

The example's retention policy does not back up message content. Keep configuration and pepper backups separate from message data, and keep the reporting private key on the workstation. Review your own retention policy before adding database, volume, or host-level backups.

## Choosing a runtime for a new VPS

For a dedicated Nylzo VPS, rootless Podman managed by systemd Quadlet is a reasonable alternative to the tested Compose example. Caddy can run as a host service and proxy to Nylzo's loopback-only published port. This keeps application containers under a dedicated unprivileged account and uses the host's service manager for startup, restart policy, and resource limits.

Rootless Quadlet requires the user's unit search path and a working user service manager, normally with lingering enabled for the dedicated service account. Adding `User=` or `DynamicUser=` to a rootful Quadlet does not make the container rootless. Use a supported Podman release and cgroup v2. Keep the default seccomp profile and the distribution's AppArmor or SELinux policy enabled.

Carry over the Compose protections: a digest-pinned verified image, read-only root filesystem, no capabilities, no new privileges, explicit memory/process limits, one writable data volume, and a separately managed pepper secret. Rootless UID mappings differ from host UIDs; do not copy the Compose `chown 65532` command to a rootless deployment. Confirm the observed proxy address and forwarding-header behavior before configuring the exact trusted proxy. Never expose Nylzo's backend port publicly.

Expose only HTTPS and the ports needed for certificate issuance. Restrict administrative SSH to a VPN or an explicit firewall allowlist, use a dedicated administrator account and strong SSH authentication, and disable password/root SSH login after testing access. Apply supported OS security updates, monitor disk space and health, and keep encrypted backups off the VPS. Test restoration of both application data and the pepper. A live SQLite database needs a consistent backup procedure; copying only its database file can lose WAL state.

A pull-based deployment agent can check for a new image digest, verify its Cosign signature and GitHub provenance against a published immutable release, and update the Quadlet's pinned image reference. Give GitHub no root or SSH credential for the VPS. Check health and reported version after a restart, and define rollback rules around database compatibility and a tested backup. The Nylzo repository does not yet provide a tested Quadlet deployment or production updater.

Coolify offers a convenient deployment dashboard, but it also adds a control plane and privileged server access to maintain. Its own security documentation requires separate host hardening, patching, and workload backups. For one Nylzo instance, choose a dashboard only if its operational convenience justifies that additional component.

- [Podman: Quadlet and rootless units](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
- [Caddy: automatic HTTPS](https://caddyserver.com/docs/automatic-https)
- [Coolify: security model](https://coolify.io/docs/core/security-model)
