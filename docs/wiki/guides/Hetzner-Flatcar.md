# Hetzner Cloud with Flatcar

**Experimental deployment profile.** The repository provides the configuration and acceptance tests. Provisioning on a real Hetzner account, public TLS, and an actual Flatcar A/B OS update still require cloud acceptance before this profile is approved for production. No cloud resources are created by CI.

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

Edit `.deploy/production/site.json`. Replace the example domain, administrator networks, operator, and contact. For nylzo.com, use `domain` and `dnsZone` equal to `nylzo.com`. Keep the generated public reporting key. Select a currently available server type and matching architecture (`cx`/`cpx`/`ccx`: amd64; `cax`: arm64). The defaults target a small server with at least 2 vCPUs, 4 GiB RAM, and 40 GB disk. Availability and prices must be checked with Hetzner; no particular plan is guaranteed to remain the cheapest.

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

The image factory starts a disposable Ubuntu server behind a firewall with no inbound rules. It verifies Flatcar's signed PXE kernel and RAM filesystem, boots them with `kexec`, and verifies the signed Hetzner disk image before writing it to the only unmounted disk. It powers off without booting the installed disk. The operator snapshots this clean image and destroys the builder in a `finally` block. A failed local process can interrupt that cleanup: inspect the project and remove any leftover builder after checking its identity. Snapshots incur storage charges until removed.

The production stack starts a separate server from that snapshot with the final Ignition configuration. OpenTofu prevents routine server deletion/rebuild. There are no messages, pepper, reporting private key, or cloud credentials in the snapshot. Changes to Ignition are first-boot changes; an existing server cannot be reconfigured by merely editing `user_data`. Review and apply later host-configuration changes explicitly over the administrator connection, or provision a replacement with a deliberate state-migration procedure.

## Cloudflare DNS without proxying

Create a Cloudflare account and a zone for the domain. Copy all required existing DNS records, including mail and verification records, before changing nameservers. Keep the registrar at Forpsi or your current registrar.

Use an account token with zone read/create permission when creating the zone, plus DNS edit, DNS settings edit, and DNSSEC edit for the intended zone. Prefer a zone-scoped token after initial setup. Set `cloudflareAccountId` and `dnsZone` in `site.json`, then save the token in a separate private file:

```sh
export CLOUDFLARE_API_TOKEN_FILE="$HOME/.config/nylzo/cloudflare-token"
chmod 0600 "$CLOUDFLARE_API_TOKEN_FILE"
mise run deploy plan dns .deploy/production/site.json
mise run deploy apply dns .deploy/production/site.json
mise run deploy outputs dns .deploy/production/site.json
```

The operator imports a matching existing zone and its instance A, AAAA, and certificate-authority records. Ambiguous records stop the operation. Other records are not managed by this stack. Review the plan carefully: DNS-only mode applies to the entire zone. The stack sets unproxied A/AAAA records, permits Let's Encrypt certificates, forbids wildcard certificate issuance, and enables zone signing.

At the registrar, delegate to the printed Cloudflare nameservers. After delegation is active, publish the printed DS record at the registrar and verify the complete DNSSEC chain. An existing DS for another DNS provider must be migrated correctly; leaving a stale DS breaks resolution. OpenTofu cannot automate registrar changes without a supported, separately authorized registrar API. Enabling signing at Cloudflare alone does not complete DNSSEC.

Caddy obtains its certificate directly from Let's Encrypt. The DNS token is never installed on the server. Cloudflare handles DNS queries; HTTPS connections go directly to Hetzner.

## Runtime and updates

Only ports 80/tcp, 443/tcp, 443/udp, ICMP, and allowlisted SSH are accepted by the cloud firewall. Caddy is an unprivileged host service with only the capability needed to bind public ports. Nylzo listens on a private internal bridge address with no published backend port, with a read-only container filesystem, no Linux capabilities, no new privileges, explicit CPU/memory/process limits, and a separate user namespace. Its internal bridge has no Internet route; host firewall rules reject new connections from that bridge. Caddy preserves client addresses and Nylzo trusts only the bridge gateway.

The pepper is generated on first boot and retained across restarts. The reporting private key stays on the operator workstation. Application/proxy access logs are disabled or bounded as described in [Self-hosting](Self-hosting.md). At 250 daily creations, seven-day retention needs up to roughly 11 GB of maximum-size messages; reserve additional space for the OS, images, reports, and logs. This profile caps configuration at 500 daily creations and stops new creation below 4 GiB free space.

With `automaticUpdates: true`, a timer checks every ten minutes, with up to one minute of jitter:

- Nylzo accepts a newer stable patch in the installed major/minor series. It requires an immutable GitHub release, the exact release workflow's Sigstore identity, GitHub provenance, and a digest-pinned image. A probe uses disposable data with no network before persistent state is touched.
- Caddy verifies its publisher's signature and SHA-512 checksum and accepts patch upgrades in its installed major/minor series. Its update runs independently of Nylzo's update.
- Host verification tools use SHA-256 pins from the reviewed initial configuration. Later Nylzo releases carry a signed `host-tools.json` manifest to refresh them transactionally; downloads are verified with the already trusted tools. A failed refresh retains the previous verified generation and prevents deployment of the candidate Nylzo release.

Minor/major upgrades require review. Host scripts and infrastructure are not automatically executed from the GitHub main branch. Flatcar uses its own signed Stable OS update mechanism and reboot window (03:00–04:00 UTC). The application health gate runs before update-engine can confirm a new boot slot; a failed new OS requests a reboot while the slot is still unconfirmed. Actual A/B recovery remains a cloud acceptance requirement.

If a candidate fails after opening persistent state, the updater stops Nylzo and writes `failed.json`. It refuses automatic retries and does not restore an old database or blindly run an older binary. Restoring old message state could resurrect consumed capabilities. Investigate the failed version and database compatibility before recovery. Caddy can restore its previous binary because its update does not migrate message state.

## Acceptance and operation

Before accepting real traffic:

1. Confirm public DNSSEC validation, certificate issuance, HTTP redirects, IPv4 and IPv6 client address handling, and refusal of public backend/SSH access.
2. Run a matching verified `nylzo verify-client --server https://your-domain`, create/read/burn synthetic messages, reboot, and confirm a burned message stays gone and the pepper stays unchanged.
3. Exercise one signed Nylzo patch update, rejected signature/provenance, a failed candidate, an actual Flatcar A/B OS update, and OS health-gate failure. Record the observed recovery behavior.
4. Connect external HTTPS/disk/update-failure monitoring and test alert delivery. The repository's `monitor` workflow can verify the public web client after setting `NYLZO_MONITOR_URL`; it does not monitor local disk, Caddy's version, or Flatcar update failures. Do not treat a running service as proof that updates succeeded.

Inspect the host through allowlisted SSH:

```sh
sudo systemctl status nylzo caddy nylzo-update.timer update-engine
sudo journalctl -u nylzo-initialize -u nylzo-update -u nylzo-ready
sudo cat /var/lib/nylzo-deploy/current.json
```

The profile deliberately disables Hetzner disk backups. Rolling back a whole-server snapshot can restore already-consumed message state. Follow the retention and recovery policy in [Operations](Operations.md); keep configuration/pepper recovery material separate from message data. Periodically inspect disk usage and remove unused deployment images only after confirming the active digest. Do not run a global Docker prune as part of a security update.

Hetzner and administrator accounts remain trusted. This profile does not provide Secure Boot, a vTPM-backed disk key, confidential computing, DDoS immunity, or protection from a malicious host operator serving altered browser code. It reduces routine maintenance; account recovery, alerts, incident response, and reviewed breaking upgrades still need an operator.

## Sources and maintenance

Versions and checksums are in [toolchain.json](../../../deploy/hetzner-flatcar/toolchain.json), `mise.lock`, and the provider locks. Dependabot proposes provider upgrades; maintainers must also review upstream verifier/Caddy pins and publish the signed host-tool manifest when updating them. The profile emits native Ignition JSON and validates it with the official validator; it does not depend on Butane.

- [OpenTofu: state and plan encryption](https://opentofu.org/docs/v1.13/language/state/encryption/)
- [Flatcar: supply-chain verification](https://www.flatcar.org/docs/latest/security/supply-chain/), [PXE provisioning](https://www.flatcar.org/docs/latest/deploy/bare-metal/booting-with-pxe/), and [update strategies](https://www.flatcar.org/docs/latest/updates-releases/releases/update-strategies/)
- [Caddy: signature verification](https://caddyserver.com/docs/signature-verification)
- [Cloudflare: DNS-only mode](https://developers.cloudflare.com/dns/proxy-status/), [DNSSEC](https://developers.cloudflare.com/dns/dnssec/), and [migration](https://developers.cloudflare.com/dns/dnssec/zone-migration/)
- [Hetzner: server security limitations](https://docs.hetzner.com/cloud/servers/faq/)
