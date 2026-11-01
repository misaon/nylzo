# Hetzner Cloud with Flatcar

**Experimental deployment profile.** Real Hetzner provisioning, TLS, a signed application update, isolation checks, and reboot persistence passed private acceptance on 28 October 2026. External alert delivery, operator recovery, and an actual Flatcar A/B OS update remain outstanding. See the [deployment security review](../maintainers/Deployment-security-review.md). No cloud resources are created by CI.

The [profile](../../../deploy/hetzner-flatcar) uses OpenTofu for infrastructure, Flatcar Stable for automatic OS updates, Docker with user namespace remapping for Nylzo, and Caddy for HTTPS. An optional Cloudflare stack manages DNS without proxying traffic and enables DNSSEC. Other DNS providers can be used independently.

This is a single-server deployment with brief restart windows, not a highly available service. Read the [security model](Security-model.md) and [operations guide](Operations.md) first.

## Before provisioning

- Create a dedicated Hetzner Cloud project. Hetzner tokens have no expiry, IP filter or narrower scope, and a Read & Write token is root on the server. Create one for a plan/apply session and delete it afterwards, or keep it in a password manager and pass it only to that command, for example `HCLOUD_TOKEN="$(op read op://nylzo/hetzner/token)" mise run deploy plan server …`. `HCLOUD_TOKEN_FILE` still works for a private `0600` file.
- Enable the strongest 2FA each provider offers on the hosting, DNS, registrar, and GitHub accounts, and keep recovery codes offline.
- Choose administrator networks: IPv4 /24 or narrower, IPv6 /64 or narrower. A single /32 or /128, or a WireGuard exit you control, is better than a shared VPN range.
- Create a hardware-backed SSH key: `ssh-keygen -t ed25519-sk -O verify-required -O resident`. Provisioning a server requires one and writes `HARDWARE_KEYS_ONLY=true` to its `/etc/nylzo/host.env`, so the server accepts only `sk-ssh-ed25519@openssh.com` keys. sshd requires the PIN (`PubkeyAuthOptions verify-required`).
- Install [mise](https://mise.jdx.dev/getting-started.html) and [age](https://github.com/FiloSottile/age), clone a reviewed Nylzo revision, and use a [verified Nylzo CLI](Verify-a-release.md).

```sh
umask 077
mkdir -p .deploy
mise run deploy init "$HOME/.ssh/id_ed25519_sk.pub" .deploy/production
```

Initialization creates the reporting key pair, a random state-encryption key and `site.json`. It encrypts the reporting private key into `report-key.age`, with a passphrase by default or to `NYLZO_REPORT_KEY_RECIPIENT`, for example a recipient from age-plugin-yubikey. The plaintext key exists only for that step. Read reports with `nylzo reports --key-file <(age --decrypt report-key.age) < report.sealed`. Back up the operator directory away from the server; losing the state key makes OpenTofu state unreadable, and losing the reporting key makes sealed reports unreadable.

Edit `site.json`:

- Replace the example domain, administrator networks, operator and contact.
- Set `initialRelease` to the latest release. Planning checks with cosign that the release workflow signed its `checksums.txt` at that tag, and that it lists the `nylzo-caddy_*` archive for your architecture and `nylzo-host_X.Y.Z.tar`.
- For nylzo.com, set `domain` and `dnsZone` to `nylzo.com`. An apex domain also gets a `www` redirect.
- Optional: `heartbeatUrl` (see [host monitoring](Host-monitoring.md)), `tang` (see [encrypted data volume](#encrypted-data-volume)) and `acmeAccountUri` (see [DNS](#cloudflare-dns-without-proxying)).
- Pick a server type and matching architecture (`cx`/`cpx`/`ccx`: amd64; `cax`: arm64), with at least 2 vCPUs, 4 GiB RAM and 40 GB disk.

## Build the image and server

```sh
mise run deploy plan image .deploy/production/site.json
mise run deploy apply image .deploy/production/site.json
mise run deploy plan server .deploy/production/site.json
mise run deploy apply server .deploy/production/site.json
```

Read each plan before applying it. Applying accepts only the saved plan, unchanged infrastructure and inputs, and a plan less than one hour old. OpenTofu state and plan encryption are enforced.

The image factory starts a disposable Ubuntu server behind a firewall with no inbound rules, verifies Flatcar's signed PXE kernel, RAM filesystem and Hetzner disk image, writes the image and powers off. The operator snapshots it and destroys the builder. The snapshot holds no messages, pepper, keys or credentials.

The server's Ignition holds only site settings, the SSH key, a few early security files, and the address and SHA-256 of the initial release's host bundle. Ignition downloads the bundle and checks that hash before first boot; `nylzo-host.service` then installs it before the network and SSH start. OpenTofu ignores later `user_data` changes, so a template edit cannot replace a live message store. Later host changes arrive through signed releases.

Keep `publicAccess: false` during acceptance: HTTPS then answers only the administrator networks, while port 80 stays public for ACME HTTP-01 and redirects. When you open the service, set `publicAccess: true`, apply a new server plan, and change `PUBLIC_ACCESS=true` in `/etc/nylzo/host.env` on the server followed by `sudo systemctl restart nylzo-firewall`. Both firewalls must agree. Do the same for a change of administrator networks (`ADMINISTRATOR_NETWORKS`, comma-separated).

## Cloudflare DNS without proxying

Create the zone, copy existing records, and keep the registrar. The registrar of record for nylzo.com is Tucows, reached through a reseller.

Give the token Zone read, DNS edit and SSL and Certificates edit for this zone only, a short TTL and a client IP filter for your address ([Cloudflare: restrict tokens](https://developers.cloudflare.com/fundamentals/api/how-to/restrict-tokens/)). Pass it the same way, as `CLOUDFLARE_API_TOKEN` from a password manager or `CLOUDFLARE_API_TOKEN_FILE`.

```sh
mise run deploy plan dns .deploy/production/site.json
mise run deploy apply dns .deploy/production/site.json
mise run deploy outputs dns .deploy/production/site.json
```

The stack manages:

- Unproxied A and AAAA records, and DNSSEC signing.
- Universal SSL off. With Universal SSL on, Cloudflare orders its own wildcard certificates and adds CAA records for five CAs that the API does not show. After applying, check with `dig CAA nylzo.com +dnssec` that only the two records below remain; if Cloudflare keeps hidden records, move the zone to a DNS-only provider.
- CAA `issue "letsencrypt.org; validationmethods=http-01; accounturi=…"` and `issuewild ";"`. Caddy uses only HTTP-01. The account URL exists after Caddy's first certificate: read it on the server with `sudo jq -r .location /var/lib/caddy/caddy/acme/acme-v02.api.letsencrypt.org-directory/users/*/*.json`, set `acmeAccountUri`, and apply again.
- A null MX, `v=spf1 -all` and `_dmarc` `p=reject; sp=reject`, because the domain sends no mail.
- A `www` CNAME for an apex domain, which Caddy redirects to the apex so HSTS preload can pass.

The operator imports matching existing records and stops on ambiguous ones. If the domain already has a DS record, complete the signed-zone migration first. Otherwise delegate to the printed nameservers, publish the printed DS at the registrar and verify the chain.

At the registrar, turn on the transfer, update and delete locks, ask for the `.com` registry lock, renew for several years with auto-renew, and secure the account with strong 2FA. The domain monitor below checks the locks and expiry.

## Runtime

- The cloud firewall admits 80/tcp, 443/tcp and 443/udp, ICMP, SSH from the administrator networks, and 8443/tcp when host monitoring is on. An nftables policy on the host admits exactly the same with input policy drop, so a detached or edited cloud firewall exposes nothing new. An iptables `connlimit` rule allows each IPv4 address and IPv6 /64 at most 64 TCP connections to the web ports; Flatcar's kernel has no nftables connection counting. New connections from the container bridge are rejected over IPv4 and IPv6.
- Caddy accepts only TLS 1.3 with X25519MLKEM768. Every target in `web/.browserslistrc` supports it (Chrome and Edge 131+, Firefox 132+, Firefox for Android 145+, Safari and iOS 26+), as do the CLI (Go 1.24+) and Node 26. Older clients cannot connect, among them Safari and iOS before 26, Firefox ESR 128, curl on Ubuntu 24.04, and many link-preview bots and uptime monitors ([transport](Security-model.md#transport)). HTTP/3 0-RTT is off, so every request comes from a validated client address. Caddy removes client-sent `X-Real-IP`, `Forwarded` and similar headers and sets `X-Forwarded-For` itself; Nylzo trusts only the bridge gateway.
- Caddy requests Let's Encrypt `shortlived` certificates (160 hours) and renews them with ACME Renewal Info. A stolen TLS key stays valid for days, not months. A Let's Encrypt outage of about two days triggers the certificate alert; one of about a week stops HTTPS.
- Caddy limits headers to 16 KB, bodies to 6 MiB, and sets `read_header 10s`, `read_body 6m`, `read_body_idle 20s`, `write 6m` and `idle 2m`. Its own error pages carry CSP, `nosniff`, HSTS and `no-store`.
- SSH key exchange is `mlkem768x25519-sha256` or `sntrup761x25519-sha512`, the latter also under its older name `sntrup761x25519-sha512@openssh.com`, so OpenSSH 8.9 and newer connect with default settings. The server offers no classical key exchange, so a client that prefers ECDH, such as the macOS 27 client, still negotiates ML-KEM. The host key is Ed25519, with AEAD ciphers and ETM MACs. sshd accepts `sk-ssh-ed25519@openssh.com` keys, and plain `ssh-ed25519` keys too unless `HARDWARE_KEYS_ONLY=true`.
- Caddy, the updater, the clock check, the heartbeat, the firewall and the Docker client each run in a systemd sandbox with a system-call filter, no user namespaces, no core dumps and no metadata-service access. Core dumps are not stored anywhere (`Storage=none`, `fs.suid_dumpable=0`).
- `90-nylzo.conf` disables kexec, io_uring, SysRq and line-discipline autoloading, hides kernel pointers and logs, and rejects redirects and source routing. Host policy checks the effective values on every boot.
- Flatcar has no NTS client: systemd-timesyncd lacks NTS and the image ships no chrony. Every 15 minutes the host compares its clock with the TLS-authenticated `Date` of GitHub, Sigstore and Let's Encrypt. If two of three disagree by more than a minute, the host reports `clock: false`, so an NTP attacker can shift message expiry by about a minute without an alert.
- Flatcar Stable 4757.2.x ships Docker 28.2.2, which carries GHSA-rg2x-37c3-w2rh, GHSA-vp62-88p7-qqf5 and GHSA-x86f-5xw2-fm2r. The first two need `docker cp` into a container or the archive API; no host script uses either, and operators must never `docker cp` into the Nylzo container. The third needs a trojaned image; images are verified by signature and provenance. Flatcar Beta 4820.1 ships Docker 29.8.0, and Stable updates arrive automatically.
- The container has no memory-lock limit (`--ulimit memlock=-1:-1`, and `LimitMEMLOCK=infinity` for containerd so the user-namespaced container may raise it), so Nylzo can lock its memory and keep secrets out of swap. The 1 GiB memory limit still caps what it uses.

SELinux runs enforcing, the pepper and data mounts get a private container label, and containerd's unused CRI and transfer plugins stay disabled. The public interface has a shared 40 Mbit/s ceiling: 13.4 TB in 31 days, below the 20 TB allowance of the reviewed plans.

## Updates

With `automaticUpdates: true`, a timer checks every ten minutes.

- A patch of the installed line installs directly. A newer minor or major installs only when its `vX.Y.0` carries signed `kind: planned` evidence, and then the host goes straight to the newest patch of that line. A host that missed a planned release window, for example v0.1.12 while v0.2.3 is a security patch of planned v0.2.0, therefore still reaches the patched line. If the newest line is not approved, the host takes the newest patch of its own line. Downgrades are refused.
- The image needs an immutable release, the release workflow's Sigstore identity for that tag, GitHub provenance, and a pinned digest. A probe runs on disposable data without network before persistent state is touched.
- `nylzo-host_X.Y.Z.tar` holds every script, unit, drop-in and configuration file of this page. The updater verifies it like the Caddy build (signed `checksums.txt`, SHA-256, provenance for the tag), rejects unexpected paths, and stages it under `/opt/nylzo/host/release-vX.Y.Z`. Activation installs each file atomically, reloads systemd, and restarts only what changed: sysctl, journald, the firewall, Caddy after `caddy validate`, timers, and Docker only if its configuration changed. If host policy or Caddy fails afterwards, the previous generation is activated again. Site values stay in `/etc/nylzo/*.env` and `site.json`. After a change to `host.env`, apply it with `sudo /opt/nylzo/runtime/activate "$(readlink -e /opt/nylzo/host/current)"`.
- Caddy comes from the release being installed; host tools from its signed `host-tools.json`. Both refuse downgrades and keep the previous verified version.
- Flatcar's signed Stable updates reboot between 03:00 and 04:00 UTC. The application health gate runs before update-engine confirms a new boot slot.

If a candidate fails after opening persistent state, the updater stops Nylzo and writes `failed.json`. It never restores an old database, which could resurrect consumed messages.

Any release signed by the release workflow reaches production within one timer period. [Dependency updates](../maintainers/Security-updates.md#human-approval-for-non-security-releases) proposes how to require a second human approval for non-security releases without delaying security patches.

### One-time step for servers provisioned before the host bundle

A server provisioned before host bundles existed, including nylzo.com, still runs the scripts its Ignition wrote. They never install a bundle, and their probe and `run` start the container without `--ulimit memlock=-1:-1`, which Nylzo now needs for `mlockall`. Until you install the bundle, the old updater tries each new release and stops at its probe, before it touches the service: the server stays on 0.2.x and `nylzo-update.service` fails every ten minutes. If the running container already has no memory-lock limit (`sudo grep 'locked memory' "/proc/$(docker inspect -f '{{.State.Pid}}' nylzo)/limits"` shows `unlimited`), the old probe passes instead and the old updater installs the release by itself.

Install the bundle once by hand, after the release:

1. Wait for a release that ships `nylzo-host_X.Y.Z.tar` and opens 0.2.x data: the [Operations guide](Operations.md#logging-and-upgrades) of that release says that `serve` upgrades a schema 5 database. A release that refuses schema 5 fails on the real data, and Caddy answers 502 until you recover as below.
2. To choose the moment of the upgrade, write `AUTOMATIC_UPDATES=false` in the block below and tell your users that their open messages will be deleted. When you are ready, run `sudo systemctl start nylzo-update.service`, then set the value back to `true` and apply it as described under [Updates](#updates).
3. Keep the Hetzner console open and run the block with that release's tag and your real `host.env` values. `PUBLIC_ACCESS` and `ADMINISTRATOR_NETWORKS` must match the cloud firewall, or the host firewall closes SSH for you too. The block stops at the first failed check.

```sh
bash -euo pipefail <<'STEP'
tag=vX.Y.Z
bundle="nylzo-host_${tag#v}.tar"
cd "$(mktemp -d)"
base="https://github.com/misaon/nylzo/releases/download/$tag"
curl -fsSLO "$base/checksums.txt" -O "$base/checksums.txt.sigstore.json" -O "$base/$bundle"
/opt/nylzo/bin/cosign verify-blob checksums.txt --bundle checksums.txt.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity "https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/$tag"
grep " $bundle\$" checksums.txt | sha256sum --check --strict
digest=$(sha256sum "$bundle" | cut -d ' ' -f 1)
curl -fsSL "https://api.github.com/repos/misaon/nylzo/attestations/sha256:$digest" | /opt/nylzo/bin/jq -ce '.attestations[].bundle' > provenance.jsonl
/opt/nylzo/bin/gh attestation verify "$bundle" --bundle provenance.jsonl --repo misaon/nylzo \
  --signer-workflow misaon/nylzo/.github/workflows/release.yml --source-ref "refs/tags/$tag" --deny-self-hosted-runners
printf 'AUTOMATIC_UPDATES=true\nPUBLIC_ACCESS=false\nWWW_REDIRECT=true\nADMINISTRATOR_NETWORKS=203.0.113.7/32\nHARDWARE_KEYS_ONLY=false\n' | sudo tee /etc/nylzo/host.env
sudo mkdir -p "/opt/nylzo/host/release-$tag"
sudo tar --extract --file "$bundle" --directory "/opt/nylzo/host/release-$tag" --no-same-owner
sudo bash "/opt/nylzo/host/release-$tag/update/activate" "/opt/nylzo/host/release-$tag"
sudo /opt/nylzo/runtime/host-policy
sudo systemctl is-active nylzo caddy nylzo-firewall
STEP
```

What follows:

- Activation restarts the firewall, containerd, Docker and Caddy, and Nylzo 0.2.x restarts on the new `run`.
- Within ten minutes, or when you start `nylzo-update.service`, the updater installs the release. It switches to the release's Caddy, probes the image on empty data, stops Nylzo and starts the release on the real data. That first start deletes every message stored before it and logs `store-upgraded`, so every link sent earlier stops working.
- Back up nothing first. The release cannot read those messages, and a copy would let them outlive their opens and expiry.
- If the release does not become healthy, the updater stops Nylzo and writes `failed.json`, and Caddy answers 502. Read `sudo journalctl -u nylzo`. If the release refused the database without changing it, go back: copy `/var/lib/nylzo-deploy/previous.json` over `current.json`, delete `failed.json` and run `sudo systemctl start nylzo`. The previous release opens the untouched database.

Then move SSH to the hardware key. Add its public key to `~core/.ssh/authorized_keys`, log in with it from a second terminal, and remove the old Ed25519 key. Set `HARDWARE_KEYS_ONLY=true` in `/etc/nylzo/host.env` and run `sudo /opt/nylzo/runtime/activate "$(readlink -e /opt/nylzo/host/current)"`. Activation refuses the setting while no `sk-ssh-ed25519@openssh.com` key is authorized. sshd applies it to the next connection, so confirm one more login before you close the old session.

## Encrypted data volume

With `tang` in `site.json`, `nylzo-storage.service` keeps `/var/lib/nylzo`, `/var/lib/nylzo-secrets`, `/var/lib/caddy` and `/var/log/journal` on a LUKS2 volume in `/var/lib/nylzo-storage.luks`. Clevis binds it to two to five [Tang](https://github.com/latchset/tang) servers with the configured threshold and removes the formatting key, so the volume opens only when the Tang servers answer. Without Tang the volume does not open, and Nylzo, Caddy and the updater do not start.

```json
"tang": { "threshold": 2, "servers": [
  { "url": "http://tang-a.example.net", "thumbprint": "<base64url SHA-256 JWK thumbprint>" },
  { "url": "http://tang-b.example.org", "thumbprint": "<…>" } ] }
```

Run the Tang servers at two other providers, firewalled to the server's IPv4 and IPv6 addresses, logging every request, and alerting on requests outside the 03:00 to 04:00 UTC reboot window. Read a thumbprint with `jose jwk thp -a S256 -i <signing key>.jwk`. After the first boot, add an offline recovery passphrase: find the loop device with `losetup -j /var/lib/nylzo-storage.luks` and the Clevis slot with `sudo clevis luks list -d <device>`, then run `sudo clevis luks pass -d <device> -s <slot> | sudo cryptsetup luksAddKey --key-file - <device>` and store the new passphrase offline.

It protects the pepper, the database, Caddy's TLS and ACME account keys, and the journal against a provider snapshot booted elsewhere, a leaked backup or disk, and a Hetzner token holder who copies the disk. It does not protect against a compromised running host, the provider reading guest memory or spoofing the server address, or a token holder who boots the same server into rescue mode while Tang still answers. Only Tang servers that answer only in the reboot window, or after an operator approves, close that gap, at the cost of availability.

The volume is created only on an empty host. On an existing server, stop Nylzo and Caddy, move the four directories aside, create `/etc/nylzo/tang.json`, start `nylzo-storage`, copy the data back, and remove the plaintext copies. Free blocks of the old disk may still hold earlier data. On a suspected snapshot, deactivate the ACME account, revoke the certificate, rotate the pepper, and rotate the Tang keys.

## Monitoring

- With `heartbeatUrl` (or `/etc/nylzo/heartbeat.url` on an existing host), the host posts to an independent dead-man's switch such as [healthchecks.io](https://healthchecks.io/) every five minutes: the URL when every check passes, `URL/fail` otherwise. Only up or down leaves the host. Missing beats raise the alert, so it does not depend on GitHub's best-effort schedule.
- The optional [host monitor](Host-monitoring.md) on 8443 serves the same coarse report to a pinned client certificate; GitHub reads it as a secondary check.
- The `domain` job of `monitor.yml` checks the CAA set, mail records, registrar locks, expiry, and every new certificate in Certificate Transparency.

## Acceptance and operation

Before accepting real traffic:

1. Confirm DNSSEC, the CAA set, certificate issuance, redirects (including `www`), IPv4 and IPv6 client addresses, and that SSH and the backend refuse other sources.
2. Run a matching verified `nylzo verify-client`, create, read and burn synthetic messages, reboot, and confirm a burned message stays gone and the pepper is unchanged.
3. Exercise a signed patch update, a host bundle update, a rejected signature, a failed candidate, a Flatcar A/B OS update and a health-gate failure.
4. Stop `nylzo-host-status.timer` for 15 minutes and confirm that the heartbeat alert arrives, then start it again.

Before the first SSH login, compare the Ed25519 host-key fingerprint that `nylzo-ssh-identity.service` prints on the Hetzner console with the one SSH offers, and pin it.

```sh
sudo systemctl status nylzo caddy nylzo-firewall nylzo-update.timer update-engine
sudo journalctl -u nylzo-host -u nylzo-initialize -u nylzo-update -u nylzo-ready
sudo cat /var/lib/nylzo-deploy/current.json; readlink /opt/nylzo/host/current
```

Hetzner backups stay off: restoring a snapshot can resurrect consumed messages. Cleanup keeps the current and previous image, Caddy, tools and host generation.

Hetzner and administrator accounts remain trusted. The profile has no Secure Boot, vTPM, confidential computing or DDoS immunity, and does not stop a malicious host operator from serving altered browser code.

## Sources and maintenance

Versions and checksums are in [toolchain.json](../../../deploy/hetzner-flatcar/toolchain.json), `mise.lock` and the provider locks. The host bundle source is [deploy/hetzner-flatcar/host](../../../deploy/hetzner-flatcar/host); `manifest` maps each file to its place on the host.

CI renders the configuration for a dummy site, validates it with `ignition-validate`, tests the scripts in containers, and boots the profile in QEMU: the image installer, then two boots of a server with the bundle of this revision embedded, two Tang servers on the runner, and the release named in `tools/deploy/check.mts`. The runner serves that release's GitHub API documents to the VM; the VM still verifies signatures and provenance.

- [OpenTofu: state and plan encryption](https://opentofu.org/docs/v1.13/language/state/encryption/)
- [Flatcar: supply-chain verification](https://www.flatcar.org/docs/latest/security/supply-chain/), [update strategies](https://www.flatcar.org/docs/latest/updates-releases/releases/update-strategies/), [LUKS](https://www.flatcar.org/docs/latest/security/encryption/luks/), [releases](https://www.flatcar.org/releases)
- [Caddy: global options](https://caddyserver.com/docs/caddyfile/options) and [tls](https://caddyserver.com/docs/caddyfile/directives/tls)
- [Let's Encrypt: CAA](https://letsencrypt.org/docs/caa/) and [profiles](https://letsencrypt.org/docs/profiles/)
- [Cloudflare: CAA records](https://developers.cloudflare.com/ssl/edge-certificates/caa-records/) and [disable Universal SSL](https://developers.cloudflare.com/ssl/edge-certificates/universal-ssl/disable-universal-ssl/)
- [Cloudflare: post-quantum support by client](https://developers.cloudflare.com/ssl/post-quantum-cryptography/pqc-support/)
- [OpenSSH release notes](https://www.openssh.com/releasenotes.html) and [sshd_config](https://man.openbsd.org/sshd_config.5)
- [Hetzner: server security limitations](https://docs.hetzner.com/cloud/servers/faq/)
- [systemd-timesyncd.conf](https://www.freedesktop.org/software/systemd/man/latest/timesyncd.conf.html), [systemd.exec](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html)
- [hstspreload.org](https://hstspreload.org/)
