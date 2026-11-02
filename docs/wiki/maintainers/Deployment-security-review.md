# Deployment security review

Reviewed **28 October 2026** for the single-server Hetzner/Flatcar profile, Nylzo **0.1.4**, and host configuration through [PR #65](https://github.com/misaon/nylzo/pull/65). This is a maintainer review with CI and live-server acceptance evidence. It is not an independent audit or a certification.

The server passed private acceptance behind an administrator-only HTTPS firewall. Public production approval is still pending the checks below.

## Verified controls

| Area | Observed result |
| --- | --- |
| Repository and CI | Required checks, CodeQL, signed commits, protected release tags, pinned Actions, and secret push protection remain enabled. Untrusted PR builds receive no explicit GitHub token; fork PRs cannot enter the coverage job with OIDC publishing permission. |
| Release identity | All 21 assets of 0.1.4 passed checksum/signature verification. Binary archives and host-tool metadata passed provenance checks. A different signing identity was rejected. The server runs the verified OCI digest. |
| Automatic application update | The normal timer installed the signed 0.1.3 → 0.1.4 patch. Client verification and synthetic message checks passed afterward. Live plaintext matched, a burned message remained deleted, and the pepper was unchanged. |
| Host integrity | Flatcar Stable 4757.2.2 uses a verified, read-only `/usr`. OS updates and the application health gate are enabled. A real reboot retained SELinux enforcement, the effective kernel policy, and service sandboxes. |
| Container isolation | The application runs without root, Linux capabilities, a writable root filesystem, host networking, or a Docker socket. User namespaces, seccomp, resource limits, and no-new-privileges are active. A different SELinux category was denied access to the actual pepper despite matching Unix ownership. |
| Network isolation | Probes from the application network namespace could not reach provider metadata, host SSH/HTTP, an external HTTPS address, or external DNS. The shared IPv4/IPv6 40 Mbit/s egress ceiling survived network reload and reboot. |
| Administration | SSH requires the administrator's public key and allowlisted source network. Host identity was checked through the authenticated provider console before pinning. Password/root login, forwarding, and RSA/SHA-1 signatures are disabled. |
| Provider resources | One small server, one clean installation snapshot, deletion/rebuild protection, and an applied firewall were confirmed. No load balancer, volume, floating IP, or disk-backup service was added. |

## Findings corrected

[PR #64](https://github.com/misaon/nylzo/pull/64) narrowed CI token and coverage permissions, moved traffic limiting before bootstrap downloads, pinned the Flatcar signing-key fingerprint, enforced private SELinux labels, and sandboxed host services.

Live reboot testing then found that Flatcar's vendor sysctl file overrode two earlier settings. [PR #65](https://github.com/misaon/nylzo/pull/65) moved the policy after that file and added effective-value checks to boot readiness. It also prevents routine OpenTofu template changes from replacing a live message store.

The host's containerd version is 2.2.5. Its unused CRI and transfer plugins are disabled and boot readiness checks reject their activation. The [CRI checkpoint advisory](https://github.com/containerd/containerd/security/advisories/GHSA-p7v4-vr35-mj6f) affects checkpoint restore through CRI; this deployment does not expose that interface or use checkpoint restore. This is an applicability restriction, not a claim that the installed version contains the upstream fix. Continue reviewing Flatcar's runtime security updates.

## Audit of 31 October 2026

An infrastructure audit of the live profile and nylzo.com found 20 issues (I5-01 to I5-20) and three from the adversarial audit (F5, F9, F10). [PR #202](https://github.com/misaon/nylzo/pull/202) fixes them in code:

- Host configuration ships as a signed release bundle and updates with rollback, instead of being written once by Ignition.
- A host that missed a planned release reaches the approved line through its signed planned base.
- TLS 1.3 with X25519MLKEM768 only, no 0-RTT, short-lived certificates, request timeouts and size limits, and security headers on Caddy's own errors.
- Post-quantum-only SSH with FIDO2 keys, a host nftables firewall with per-source connection limits, systemd sandboxes, hardened kernel settings and no core dumps.
- An optional LUKS2 data volume unlocked through Tang, a TLS-authenticated clock check, a certificate check, an outbound heartbeat, and a domain monitor for CAA, mail records, registrar locks and Certificate Transparency.
- DNS code for CAA bound to the ACME account and HTTP-01, null MX, SPF, DMARC, `www`, and Universal SSL off.

What the owner still applies by hand is listed in the [Hetzner Flatcar guide](../guides/Hetzner-Flatcar.md): the one-time host bundle installation on the existing server after a release that upgrades 0.2.x data, a hardware SSH key followed by `HARDWARE_KEYS_ONLY=true`, the DNS apply and CAA check, the registrar lock, the heartbeat and Tang servers, and revocation of the two Cloudflare-held wildcard certificates (Let's Encrypt through an account holding valid authorizations for both names, Sectigo through Sectigo).

## Live checks of 2 November 2026

The operator supplied read-only diagnostics from the existing VPS. These checks establish the observations below, not completion of public production acceptance. The application update timer was deliberately stopped during diagnosis and still needs to be restored after the failed rollout is resolved.

- The host reports Flatcar Stable **4757.2.3** and deployment metadata **v0.2.3**. The signed **v0.3.0** host bundle passed checksum, signing-identity and provenance verification, but it has not been activated: the host-bundle pointer was absent. Source configuration and live configuration must be assessed separately.
- SELinux enforcement, the container's matching private labels, user-namespace mapping, zero effective capabilities, seccomp and no-new-privileges were observed. The configured limits are 1 GiB RAM, 1.5 CPUs and 128 tasks. No active swap was present. TCP connection attempts from the application network namespace to host SSH/HTTP, provider metadata and an external HTTPS address all failed to connect. This repeat did not test DNS or every possible destination. The earlier Bash `/dev/tcp` attempt was invalid because the redirection was unavailable.
- The applied provider firewall restricts SSH and public HTTPS to the administrator's address, while allowing public HTTP and the mTLS monitoring listener. The host's separate default-drop nftables input policy was absent. The monitoring listener rejected a client without a certificate; successful authenticated monitoring and alert delivery remain unverified.
- The running application has a finite **8 MiB** locked-memory limit. SSH still permits stream-local forwarding and uses the older authentication-attempt and login-grace limits. Kernel kexec, io_uring, SysRq and dumpability settings also differ from the current host profile. The global coredump drop-in was absent, although the application's effective core-size limit is zero. These findings require a controlled host upgrade; green legacy policy results do not establish current policy compliance.
- The old status generator reports five checks and omits the current clock and certificate checks. Heartbeat units were not found. Both the deployed generator and current source used one `systemctl is-active` call for several units, which succeeds when **any** unit is active. The source now checks each required unit separately, with regression cases for each unavailable required unit while the others stay active. The VM rollback acceptance check had the same aggregation mistake and now checks units individually too.
- The failing `systemd-sysupdate-reboot` timer sees only a placeholder transfer definition. Update-engine and locksmithd are running and wanted by `multi-user.target`; update-engine completed an update check with no offered OS update. The source now masks and stops the unused sysupdate reboot timer while preserving both Flatcar update services. Monitoring also requires locksmithd to be active.
- The unused containerd podsandbox controller and restart monitor failed to initialize because their in-memory client requires the intentionally disabled transfer service. The source now disables these plugins explicitly and readiness rejects their registration, including error states. Application restarts remain supervised by `nylzo.service` with `Restart=on-failure`.
- The extended VM acceptance test exposed a lifecycle bug: restarting Docker stopped the completed initialization unit and its dependent readiness and updater units. Initialization now starts Docker through `Wants=` and verifies it is active before any initialization work, without tying the completed one-shot unit to the daemon's lifetime. Required storage, tooling and bandwidth dependencies remain enforced. Activation includes the application's conditional restart in the same transaction as the runtime services, so it waits for that restart job instead of returning while it is still being processed. The VM test deliberately changes containerd configuration, verifies the application afterwards, and repeats those checks after rollback.

No host bundle or application upgrade was applied during these diagnostics. Publish and verify the corrected host bundle before applying it; keep ordinary SSH-key access until a hardware key has been obtained and tested. The v0.3.0 data transition discards older protocol messages, as described in the [operations guide](../guides/Operations.md).

### Container-runtime review still open

The observed Docker Engine **28.2.2** and containerd **2.2.5** version strings are not proof of security patch status. The Flatcar stable-branch package definitions refer to these upstream source releases, but a complete review of the exact shipped build and any backports remains outstanding. Treat the following as deployment restrictions and open patch questions, not as evidence that the installed binaries contain upstream fixes:

- [Docker's registry DNS advisory](https://github.com/moby/moby/security/advisories/GHSA-7cfq-22r6-qp73) is fixed in **29.8.2**. A mixed loopback and remote DNS answer can make registry requests permit invalid certificates or HTTP fallback. Empty `insecure-registries` configuration does not remove Docker's implicit loopback exception. Digest pinning is useful against image substitution but is not a complete workaround. Before public launch, establish a patched engine or a verified workaround from the advisory, such as trusted registry-name resolution or restricted daemon egress.
- [The containerd Content-Length crash](https://github.com/containerd/containerd/security/advisories/GHSA-8647-2jxj-p2rc) requires enabled concurrent layer-fetch buffering. [The malformed-digest crash](https://github.com/containerd/containerd/security/advisories/GHSA-vwhp-8p66-mq4f) uses the transfer service. This profile disables CRI and transfer, and Docker reports the classic `overlay2` image store. The operator confirmed that Docker's configured socket path and the inspected containerd socket resolve to the same socket file, served by the system containerd process. The disabled transfer interface restricts these attack paths; it does not establish that the binaries include the upstream patches.
- [The unbounded-layer disk exhaustion advisory](https://github.com/containerd/containerd/security/advisories/GHSA-hc94-p972-ch66) also concerns image ingestion. Only signed, provenance-verified release images are admitted here, but review the actual pull path and patch status rather than extrapolating from the disabled CRI interface. The upstream 2.2 fixes for these three image-ingestion advisories are in **2.2.10**.
- The Docker archive API race advisories require operations such as copying into a running container. Do not use `docker cp` into the running application. The acceptance fixture copies the executable out of a newly created, unstarted container. Swarm is inactive and no Docker authorization plugins are configured; those observations restrict the applicability of other advisories, without establishing a general clean bill of health.

## Outstanding acceptance

- **DNSSEC:** The DS record is published at `.com` and the chain validates (checked 31 October 2026). Keep the domain monitor enabled to catch drift.
- **OS recovery:** An ordinary reboot passed. A real update between two Flatcar versions and recovery from a failed new A/B slot have not been exercised on this VPS. The current installation already matches the reviewed Stable release; downgrading the live host solely to create a test is not an acceptance result.
- **Monitoring:** Configure the heartbeat and observe one failure notification. Enable the client and domain monitors after public HTTPS opens. A green service status does not establish alert delivery.
- **Operator recovery:** Keep an encrypted copy of the operator keys and state away from the workstation and VPS. Off-device recovery has not yet been accepted. Do not use a message-database rollback as a substitute: it can restore consumed capabilities.

This profile has restart windows and a single host. The provider and administrator remain trusted. It does not provide hardware-attested boot, protection from a malicious host, or high availability. Read the [security model](../guides/Security-model.md) for browser-delivery and recipient-device limits. The [assessment](Security-assessment.md) also records why one maintainer cannot satisfy independent human approval on every primary-branch change.

Repeat the affected acceptance checks when host policy, runtime versions, release authority, network boundaries, or persistent-state handling change. Keep raw operational evidence private; publish conclusions and reproducible tests without credentials, message links, or user data.
