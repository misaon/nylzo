# Self-hosting

The [Compose example](../../deploy/compose) runs Nylzo behind Caddy with TLS. Use a Linux server with Docker Engine, Docker Compose v2, a domain pointing to the server, and ports 80/tcp, 443/tcp, and 443/udp open.

The TLS endpoint sees ciphertext, request signatures, and leases, and can replace the web client. Terminate TLS on infrastructure you trust with the server itself. Read the [security model](Security-model.md) and [operations guide](../operations.md) before deployment.

## Prepare the deployment

1. Download and [verify a release](Verify-a-release.md). Verify the container image's signature and provenance against the selected tag and pin its digest.
2. Copy [compose.yaml](../../deploy/compose/compose.yaml), [Caddyfile](../../deploy/compose/Caddyfile), and [.env.example](../../deploy/compose/.env.example) into a private deployment directory. Copy `.env.example` to `.env`.
3. Set `NYLZO_IMAGE` to the digest-pinned reference printed by the verification guide. Set `NYLZO_DOMAIN` to your lowercase domain and `NYLZO_GLOBAL_DAILY_CREATES` to a cap your disk can support.
4. Fill in `NYLZO_OPERATOR`, `NYLZO_CONTACT` (email or HTTPS address), and `NYLZO_HOSTING`. They appear on `/transparency`. Compose derives `NYLZO_PUBLIC_URL` from the domain for canonical URLs, previews, and the sitemap.

Generate a server pepper in the deployment directory. The container runs as UID 65532 and must be the only account able to read the file:

```sh
umask 077
mkdir -p secrets
openssl rand -hex 32 > secrets/pepper
sudo chown 65532:65532 secrets/pepper
sudo chmod 0400 secrets/pepper
```

Run this only for a new deployment; do not overwrite an existing pepper during an upgrade. Keep it outside the database. Losing the pepper makes existing tokens unverifiable and live messages inaccessible. A leaked pepper combined with server data enables additional guessing attacks; see the [protocol](../../spec/protocol.md).

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

Takedown deletes the server's message blob without reading its content. Use an optional reporter contact only for acknowledgement and the moderation decision. Delete decrypted reports after review; they contain message IDs and contact addresses. See [Operations](../operations.md#abuse-reports-and-takedown) for retention, quotas, and sanitization.

## Upgrade and rotate keys

Verify the new image against its exact tag, update the digest-pinned `NYLZO_IMAGE`, and run `docker compose up -d`. Do not overwrite the pepper or reporting key. Read the [operations guide](../operations.md#logging-and-upgrades) for schema compatibility.

For pepper rotation, keep the previous pepper as a second Docker secret and configure `NYLZO_PEPPER_PREVIOUS_FILE`, then generate the new pepper. Retain the previous pepper for seven days so live messages remain accessible. See [Operations](../operations.md#configuration).

The example's retention policy does not back up message content. Keep configuration and pepper backups separate from message data, and keep the reporting private key on the workstation. Review your own retention policy before adding database, volume, or host-level backups.
