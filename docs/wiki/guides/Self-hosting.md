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

Generate a server pepper in the deployment directory. The container runs as UID 65532 and must be the only account able to read the file:

```sh
umask 077
mkdir -p secrets
openssl rand -hex 32 > secrets/pepper
sudo chown 65532:65532 secrets/pepper
sudo chmod 0400 secrets/pepper
```

Run this only for a new deployment; do not overwrite an existing pepper during an upgrade. Keep it outside the database. Losing the pepper makes existing tokens unverifiable and live messages inaccessible. A leaked pepper combined with server data enables additional guessing attacks; see the [protocol](../reference/Protocol.md).

On your own workstation, create the abuse-reporting key with a verified CLI:

```sh
nylzo report-key --key-file "$HOME/nylzo-report-key"
```

Copy the printed **public** `NYLZO_REPORT_KEY=…` value into `.env`. Keep the private key on the workstation and off the server; it is needed to decrypt reports.

Start from the deployment directory:

```sh
docker compose up -d
```

Check the HTTPS instance and its `/transparency` page. Run `nylzo verify-client --server https://your-domain.example` with the matching verified release; understand the [limits of that check](Security-model.md#the-client-is-part-of-the-trust-boundary).

## What the example configures

- Nylzo has a read-only root filesystem, drops all Linux capabilities, and sets `no-new-privileges`. Only `nylzo-data` is writable. The pepper is a Docker secret file, rather than a process environment value.
- Caddy has the same restrictions and runs as UID 65532. Its container sets `net.ipv4.ip_unprivileged_port_start` to 80, which lets it bind ports 80 and 443 without `NET_BIND_SERVICE`. Only `caddy-state`, which holds certificates and the ACME account, is writable.
- The network has IPv4 and IPv6. Caddy uses fixed addresses `172.30.80.2` and `fd1f:9791:e50b::2`. Nylzo trusts only those proxy addresses, and Caddy replaces client-supplied forwarding headers. Other containers use the upper half of each subnet. If these ranges conflict with your network, change the subnets, Caddy addresses, and trusted proxies together.
- Docker Engine's default IPv6 firewall support is required. Without the IPv6 network, Docker's userland proxy can make all IPv6 users share one source-address rate-limit bucket. Docker Desktop also proxies incoming traffic and can collapse source addresses; use Linux with Docker Engine for production.
- The global daily cap defaults to 2,000 creations. Proof of work starts at 1,000; at the cap, creation returns 503 while reading continues. Address limits reset with the process, so the global cap protects disk capacity against clients rotating addresses.
- A maximum-size message is about 5.4 MB and may live for seven days. Budget up to eight daily caps of stored messages: about 86 GB at 2,000 creations per day. Adjust the cap for your disk. Below the 1 GiB free-space reserve, creation stops while reading, cancellation, and deletion continue.
- Both containers use the `local` log driver with at most three 10 MB files. Caddy's access logging and error loggers containing request details are disabled, preventing proxy failures from logging addresses, message paths, or `X-Nylzo-*` headers. Startup and certificate logs remain.
- Caddy reuses idle upstream connections for at most 25 seconds, below Nylzo's 30-second idle timeout. It removes `Server` and `Via` headers, including on errors and HTTP redirects, and closes requests for other hosts without an HTTP response. Nylzo supplies CSP and the remaining security headers.

## Process abuse reports

The server stores reports sealed to the operator's public key in daily UTC files under `abuse/`. The image has no shell. Copy reports to your workstation:

```sh
docker compose cp nylzo:/home/nonroot/abuse/. ./abuse
```

Decrypt them on the workstation with the private reporting key:

```sh
cat abuse/*.sealed | nylzo reports --key-file "$HOME/nylzo-report-key"
```

After review, place approved message IDs in a private file, one per line. Run from the deployment directory:

```sh
docker compose exec -T nylzo /ko-app/nylzo takedown < approved-ids.txt
```

Takedown deletes the server's message blob without reading its content. Use an optional reporter contact only for acknowledgement and the moderation decision. Delete decrypted reports after review; they contain message IDs and contact addresses. See [Operations](Operations.md#abuse-reports-and-takedown) for retention, quotas, and sanitization.

## Upgrade and rotate keys

Verify both new images against their exact tag, update the digest-pinned `NYLZO_IMAGE` and `NYLZO_CADDY_IMAGE`, and run `docker compose up -d`. Do not overwrite the pepper or reporting key. Read the [operations guide](Operations.md#logging-and-upgrades) for schema compatibility.

A deployment that still runs the official `caddy` image must also take the new `compose.yaml`. Caddy then runs as UID 65532 with its state in the new `caddy-state` volume and requests a new certificate on first start. Once HTTPS works, remove the old volumes with `docker volume rm nylzo_caddy-data nylzo_caddy-config`.

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
