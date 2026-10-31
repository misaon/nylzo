# Hetzner Cloud with Flatcar

**Experimental deployment profile.** Real Hetzner provisioning, TLS, a signed application update, isolation checks, and reboot persistence passed private acceptance on 28 October 2026. Public DNSSEC, external alert delivery, operator recovery, and an actual Flatcar A/B OS update remain outstanding. See the [deployment security review](../maintainers/Deployment-security-review.md). No cloud resources are created by CI.

The [profile](../../../deploy/hetzner-flatcar) uses OpenTofu for infrastructure, Flatcar Stable for automatic OS updates, Docker with user namespace remapping for Nylzo, and Caddy for HTTPS. An optional Cloudflare stack manages DNS without proxying traffic and enables DNSSEC. Other DNS providers can be used independently.

This is a single-server deployment with brief restart windows, not a highly available service. Read the [security model](Security-model.md) and [operations guide](Operations.md) first.

## Before provisioning

Create a dedicated Hetzner Cloud project and a read/write API token scoped to that project. Enable strong MFA on the hosting, DNS, registrar, and GitHub accounts. Choose an administrator IP or VPN exit range: IPv4 /24 or narrower, IPv6 /64 or narrower. SSH will be reachable only from these ranges and will require an Ed25519 key; root and password logins are disabled.

Install [mise](https://mise.jdx.dev/getting-started.html), clone a reviewed Nylzo revision, and use a [verified Nylzo CLI](Verify-a-release.md). Run from the repository root:

```sh
umask 077
mkdir -p .deploy
mise run deploy init "$HOME/.ssh/nylzo_admin.pub" .deploy/production
```

The destination must not already exist. `NYLZO_CLI` can point to the verified CLI if it is not on `PATH`. Initialization creates a reporting key pair, a random state-encryption key, and `site.json`. Back up the private operator directory securely, away from the server. Losing the encryption key makes the OpenTofu state unreadable; losing the reporting key makes sealed abuse reports unreadable.

Edit `.deploy/production/site.json`. Replace the example domain, administrator networks, operator, and contact. Set `initialRelease` to the tag of the latest release; its assets must include the `nylzo-caddy_*` archives, and `deploy plan` and `deploy apply` check that before they touch the image or server. For nylzo.com, use `domain` and `dnsZone` equal to `nylzo.com`. Keep the generated public reporting key. Select a currently available server type and matching architecture (`cx`/`cpx`/`ccx`: amd64; `cax`: arm64). The defaults target a small server with at least 2 vCPUs, 4 GiB RAM, and 40 GB disk. Availability and prices must be checked with Hetzner; no particular plan is guaranteed to remain the cheapest.

Save the Hetzner token in a private file, outside the checkout, using your editor. Do not put it in `site.json`, a command argument, or a GitHub secret for this deployment:

```sh
export HCLOUD_TOKEN_FILE="$HOME/.config/nylzo/hetzner-token"
chmod 0600 "$HCLOUD_TOKEN_FILE"
```

The file must belong to your account and must not be a symlink. The operator also accepts `HCLOUD_TOKEN` from an existing local secret manager. Tokens are passed only to the local provider/API process, never to the VPS.

## Build the image and server

```sh
mise run deploy plan image .deploy/production/site.json
mise run deploy apply image .deploy/production/site.json
mise run deploy plan server .deploy/production/site.json
mise run deploy apply server .deploy/production/site.json
```

Read each plan before applying it. Applying accepts only the saved plan, unchanged infrastructure and inputs, and a plan less than one hour old. OpenTofu state and plan encryption are enforced. Provider versions and checksums are committed for Linux and macOS on both architectures. Private working files stay under the ignored operator directory.

The image factory starts a disposable Ubuntu server behind a firewall with no inbound rules. Boot downloads use its disk rather than the small `/run` memory filesystem. It verifies Flatcar's signed PXE kernel and RAM filesystem, boots them with `kexec`, and verifies the signed Hetzner disk image before writing it to the only unmounted disk. It powers off without booting the installed disk. The operator snapshots this clean image and destroys the builder in a `finally` block. A failed local process can interrupt that cleanup: inspect the project and remove any leftover builder after checking its identity. Snapshots incur storage charges until removed.

The production stack starts a separate server from that snapshot with the final Ignition configuration. OpenTofu prevents routine server deletion/rebuild. There are no messages, pepper, reporting private key, or cloud credentials in the snapshot. Ignition is consumed on first boot; OpenTofu ignores later `user_data` changes so a template edit cannot schedule replacement of a live message store. Review and apply later host-configuration changes explicitly over the administrator connection, or provision a replacement with a deliberate state-migration procedure.

Keep `publicAccess: false` during acceptance. HTTPS is then reachable only from the administrator networks. Port 80 remains public for ACME HTTP challenges and redirects; it does not expose the application API. After completing the checks below, set `publicAccess: true` and review/apply a new server plan to open HTTPS to visitors. This changes the firewall without replacing the server.

## Cloudflare DNS without proxying

Create a Cloudflare account and a zone for the domain. Copy all required existing DNS records, including mail and verification records, before changing nameservers. Keep the registrar at Forpsi or your current registrar.

Use zone read/write and DNS write permissions, restricted to the intended zone. DNS write also covers DNSSEC and zone DNS settings. Creating a zone through OpenTofu needs a token allowed to create it in the intended account; narrow it to the zone after setup. Set `cloudflareAccountId` and `dnsZone` in `site.json`, then save the token in a separate private file:

```sh
export CLOUDFLARE_API_TOKEN_FILE="$HOME/.config/nylzo/cloudflare-token"
chmod 0600 "$CLOUDFLARE_API_TOKEN_FILE"
mise run deploy plan dns .deploy/production/site.json
mise run deploy apply dns .deploy/production/site.json
mise run deploy outputs dns .deploy/production/site.json
```

The operator imports a matching existing zone and its instance A, AAAA, and certificate-authority records. Ambiguous records stop the operation. Other records are not managed by this stack. The stack sets unproxied A/AAAA records, permits Let's Encrypt certificates, forbids wildcard certificate issuance, and enables zone signing. Set any existing CNAME records that point to the instance to DNS only as well. The Free plan supports this record-level setting; the account-wide custom DNS-only mode is unavailable on some accounts and is not required.

If the domain already has a DS record, complete the signed-zone migration procedure before changing its delegation; an incompatible DS breaks resolution. For a previously unsigned domain, delegate to the printed Cloudflare nameservers, wait for activation, then publish the printed DS record at the registrar and verify the complete DNSSEC chain. OpenTofu cannot automate registrar changes without a supported, separately authorized registrar API. Enabling signing at Cloudflare alone does not complete DNSSEC.

Caddy obtains its certificate directly from Let's Encrypt. The DNS token is never installed on the server. Cloudflare handles DNS queries; HTTPS connections go directly to Hetzner.

## Runtime and updates

Only ports 80/tcp, 443/tcp, 443/udp, ICMP, and allowlisted SSH are accepted by the cloud firewall. Caddy is an unprivileged host service with only the capability needed to bind public ports. Nylzo listens on a private internal bridge address with no published backend port, with a read-only container filesystem, no Linux capabilities, no new privileges, explicit CPU/memory/process limits, and a separate user namespace. Its internal bridge has no Internet route; host firewall rules reject new connections from that bridge. Caddy preserves client addresses and Nylzo trusts only the bridge gateway.

SELinux must run in enforcing mode. Data and pepper mounts receive a private container label; even a second container with the same Unix ownership must be denied access to the running instance's pepper. The profile uses ext4 for writable application data. Caddy and the Docker client have separate systemd sandboxes. SSH rejects RSA/SHA-1 signatures while retaining the vendor's modern key-exchange defaults.

Containerd retains Flatcar's vendor configuration and disables the unused CRI and transfer plugins. This profile runs Docker, not a Kubernetes node. Images come from the verified Nylzo release repository. Host readiness checks reject re-enabled interfaces, missing Docker isolation, an inactive traffic ceiling, or weakened kernel settings before an OS boot is accepted.

The kernel policy is installed as `90-nylzo.conf`, after Flatcar's `65-baselayout.conf`. It limits kernel information disclosure, rejects network redirects and source routing, and enables loose reverse-path validation without disabling IPv6 router discovery. Acceptance checks read the effective values on both boots; the presence of a configuration file is insufficient.

The pepper is generated on first boot and retained across restarts. The reporting private key stays on the operator workstation. Application/proxy access logs are disabled or bounded as described in [Self-hosting](Self-hosting.md). At 250 daily creations, seven-day retention needs up to roughly 11 GB of maximum-size messages; reserve additional space for the OS, images, reports, and logs. This profile caps configuration at 500 daily creations and stops new creation below 4 GiB free space.

The public network interface has a shared 40 Mbit/s outbound ceiling for IPv4 and IPv6, including HTTPS, SSH, and host updates. Continuous use for 31 days transfers about 13.4 TB, below the 20 TB allowance of the reviewed EU plans. The ceiling uses preinstalled utilities and must be active before verification tools download or public services start. A systemd-networkd configuration retains it across network reloads and reboots. Verify the allowance for your chosen plan and monitor provider usage: this host policy is not a provider-enforced billing cap and cannot protect against a compromised host.

With `automaticUpdates: true`, a timer checks every ten minutes, with up to one minute of jitter:

- Nylzo accepts a newer stable patch in the installed major/minor series. It requires an immutable GitHub release, the exact release workflow's Sigstore identity, GitHub provenance, and a digest-pinned image. A probe uses disposable data with no network before persistent state is touched.
- Caddy comes from the Nylzo release being installed, built from [deploy/caddy](../../../deploy/caddy) by the same release workflow. The updater requires an immutable release and verifies its signed `checksums.txt` against that workflow's Sigstore identity, the archive's SHA-256 and its GitHub provenance, then validates the Caddyfile before it restarts Caddy. It refuses a release older than the one the running Caddy came from, and a release build whose Caddy version is older than the official Caddy it would replace. A release whose Caddy binary is byte-identical to the running one is recorded without a restart.
- A release without a Caddy build, such as a patch on an older maintenance line, keeps a release-built Caddy in place. A host that still runs the official Caddy gets the version that release's signed `host-tools.json` names, verified against the Caddy project's release workflow identity and SHA-512 checksum, and never older than the installed one. First provisioning has no such fallback and stops unless `initialRelease` ships a Caddy build.
- Host verification tools use SHA-256 pins from the reviewed initial configuration. Later Nylzo releases carry a signed `host-tools.json` manifest to refresh them transactionally; downloads are verified with the already trusted tools. A failed refresh retains the previous verified generation and prevents deployment of the candidate Nylzo release.

Minor/major upgrades require review. Host scripts and infrastructure are not automatically executed from the GitHub main branch. Flatcar uses its own signed Stable OS update mechanism and reboot window (03:00–04:00 UTC). The application health gate runs before update-engine can confirm a new boot slot; a failed new OS requests a reboot while the slot is still unconfirmed. Actual A/B recovery remains a cloud acceptance requirement.

If a candidate fails after opening persistent state, the updater stops Nylzo and writes `failed.json`. It refuses automatic retries and does not restore an old database or blindly run an older binary. Restoring old message state could resurrect consumed capabilities. Investigate the failed version and database compatibility before recovery. Caddy can restore its previous binary because its update does not migrate message state.

Ignition runs only on first boot, so a server provisioned before the release Caddy build keeps its original scripts. They install the official Caddy release named in each release's `host-tools.json`, which had unfixed high and critical vulnerabilities in October 2026. Releases still name that version, so such a server keeps accepting Nylzo updates. Apply the reviewed `runtime/update` and `runtime/clean` to it over the administrator connection, or provision a replacement. With the new scripts, the server keeps receiving official Caddy patches and switches to the release build at the first Nylzo update that ships one. If the installed release already ships it, switch now with `sudo /opt/nylzo/runtime/update caddy vX.Y.Z`, using the tag from `/var/lib/nylzo-deploy/current.json`. The command accepts no other tag, so it cannot move Caddy ahead of the installed Nylzo release.

## Authenticated host monitoring

The optional [host monitor](Host-monitoring.md) adds a TLS 1.3 listener on TCP 8443, accessible only with the pinned client certificate. It serves a coarse, read-only report of host policy, services, free-space reserve, and update-check state. Only the public client certificate is installed on the VPS; the private identity stays with the operator and in a separate, main-only GitHub environment.

This listener can be enabled while `publicAccess` remains false. GitHub checks it about every 15 minutes without opening application HTTPS or SSH to its runners. The separate client and synthetic-message checks remain hourly. Follow the monitor guide for certificate rotation, stale-report rejection, and an observed failure/recovery test before relying on notifications.

## Acceptance and operation

Before accepting real traffic:

1. Confirm public DNSSEC validation, certificate issuance, HTTP redirects, IPv4 and IPv6 client address handling, and refusal of public backend/SSH access.
2. Run a matching verified `nylzo verify-client --server https://your-domain`, create/read/burn synthetic messages, reboot, and confirm a burned message stays gone and the pepper stays unchanged. Check that `nylzo-bandwidth.service` is active and the public interface retains its 40 Mbit/s TBF queue after a network reload and reboot.
3. Exercise one signed Nylzo patch update, rejected signature/provenance, a failed candidate, an actual Flatcar A/B OS update, and OS health-gate failure. Record the observed recovery behavior.
4. Connect [host monitoring](Host-monitoring.md) and test alert delivery. After setting `NYLZO_MONITOR_URL`, the repository's hourly `monitor` workflow also verifies the public web client and creates, decrypts, and consumes a synthetic five-minute message. This uses about 24 creations per day; account for it in the daily quota. Capability links stay in private temporary files and are removed without artifact upload. Check the host report and upstream release state together: an active updater alone does not prove that every patch is installed.

Inspect the host through allowlisted SSH:

Before the first SSH login, open the server's console from the authenticated Hetzner account. After initialization, `nylzo-ssh-identity.service` prints the Ed25519 host-key fingerprint there. Compare it with the key offered by SSH, then pin the matching key in your local known-hosts file. A fingerprint obtained only with `ssh-keyscan` over the same network is not an independent identity check. Keep strict host-key checking enabled.

```sh
sudo systemctl status nylzo caddy nylzo-update.timer update-engine
sudo journalctl -u nylzo-initialize -u nylzo-update -u nylzo-ready
sudo cat /var/lib/nylzo-deploy/current.json
```

The profile deliberately disables Hetzner disk backups. Rolling back a whole-server snapshot can restore already-consumed message state. Follow the retention and recovery policy in [Operations](Operations.md); keep configuration/pepper recovery material separate from message data. After a successful Nylzo update, cleanup retains the current and previous Nylzo image, Caddy binary, and verification-tool generation. It touches only this deployment's image repository and directories. Periodically inspect disk usage; failed candidate evidence is retained for investigation. Do not run a global Docker prune as part of a security update.

Hetzner and administrator accounts remain trusted. This profile does not provide Secure Boot, a vTPM-backed disk key, confidential computing, DDoS immunity, or protection from a malicious host operator serving altered browser code. It reduces routine maintenance; account recovery, alerts, incident response, and reviewed breaking upgrades still need an operator.

## Sources and maintenance

Versions and checksums are in [toolchain.json](../../../deploy/hetzner-flatcar/toolchain.json), `mise.lock`, and the provider locks. Renovate tracks providers, tool versions and binary checksums. Qualifying changes follow the [dependency update policy](../maintainers/Security-updates.md); signed Nylzo release evidence authorizes the host-tool versions, and each release carries its own Caddy build. The profile emits native Ignition JSON and validates it with the official validator; it does not depend on Butane.

CI boots the profile in QEMU for every change to it: the image installer, then two boots of a server provisioned from the release named in `tools/deploy/check.mts`. GitHub allows 60 unauthenticated API requests per hour per address, and hosted runners share addresses. The runner therefore reads that release's API document and provenance with its own read-only token and serves them to the VM over QEMU user networking. Only the transport changes: the VM still checks the release's immutability, signatures and provenance against those documents, and no credential enters it. If that release ships a Caddy build, the VM starts with no Caddy and installs the build. Otherwise it starts as a server on the official Caddy and installs the official release that `host-tools.json` names, verified like on a real server. Each boot must finish within ten minutes.

- [OpenTofu: state and plan encryption](https://opentofu.org/docs/v1.13/language/state/encryption/)
- [Flatcar: supply-chain verification](https://www.flatcar.org/docs/latest/security/supply-chain/), [PXE provisioning](https://www.flatcar.org/docs/latest/deploy/bare-metal/booting-with-pxe/), and [update strategies](https://www.flatcar.org/docs/latest/updates-releases/releases/update-strategies/)
- [Caddy: build from source](https://caddyserver.com/docs/build)
- [Cloudflare: DNS-only mode](https://developers.cloudflare.com/dns/proxy-status/), [DNSSEC](https://developers.cloudflare.com/dns/dnssec/), and [migration](https://developers.cloudflare.com/dns/dnssec/dnssec-active-migration/)
- [Hetzner: server security limitations](https://docs.hetzner.com/cloud/servers/faq/)
- [systemd-networkd: token bucket traffic shaping](https://www.freedesktop.org/software/systemd/man/254/systemd.network.html#%5BTokenBucketFilter%5D%20Section%20Options)
