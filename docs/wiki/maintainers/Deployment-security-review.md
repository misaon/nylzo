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

## Outstanding acceptance

- **DNSSEC:** Cloudflare signs the zone, but the registrar has not yet published the matching parent DS record. Complete the delegation-cache migration and verify the entire chain before public approval.
- **OS recovery:** An ordinary reboot passed. A real update between two Flatcar versions and recovery from a failed new A/B slot have not been exercised on this VPS. The current installation already matches the reviewed Stable release; downgrading the live host solely to create a test is not an acceptance result.
- **Monitoring:** Enable the repository's external client/lifecycle monitor after public HTTPS opens. Local disk and updater-failure alert delivery still need an external monitoring arrangement and an observed failure notification. A green service status does not establish successful alert delivery.
- **Operator recovery:** Keep an encrypted copy of the operator keys and state away from the workstation and VPS. Off-device recovery has not yet been accepted. Do not use a message-database rollback as a substitute: it can restore consumed capabilities.

This profile has restart windows and a single host. The provider and administrator remain trusted. It does not provide hardware-attested boot, protection from a malicious host, or high availability. Read the [security model](../guides/Security-model.md) for browser-delivery and recipient-device limits. The [assessment](Security-assessment.md) also records why one maintainer cannot satisfy independent human approval on every primary-branch change.

Repeat the affected acceptance checks when host policy, runtime versions, release authority, network boundaries, or persistent-state handling change. Keep raw operational evidence private; publish conclusions and reproducible tests without credentials, message links, or user data.
