# Infrastructure roadmap

Where the Hetzner deployment goes next, so that no change ever needs SSH and the host logic is typed, tested code instead of shell scripts. Research and sources: the CI/CD review of 7 November 2026.

## Today

- Releases reach the host by a signed pull: the updater verifies the Sigstore signature, GitHub provenance and checksums of every artifact before it installs it.
- `infrastructure.yml` plans and applies the OpenTofu stacks from `main` ([Hetzner Flatcar](../guides/Hetzner-Flatcar.md#deploy-from-github-actions)).
- The production watch can force maintenance through a signed directive and close the cloud firewall ([host monitoring](../guides/Host-monitoring.md#production-watch)).
- Still manual: host settings that live in Ignition (`host.env`, heartbeat URL, monitor certificate, `HARDWARE_KEYS_ONLY`), the first host bundle on servers older than the bundle, encrypted volume migration, and recovery after a failed candidate.
- Host logic: 21 bash scripts, about 1,100 lines, plus cosign, gh and jq downloaded through their own tool channel.

## Target

- **Replaceable server.** Data on an `hcloud_volume` and addresses on two `hcloud_primary_ip` with `auto_delete = false`, both with delete protection. The server itself has no SSH key and no protection, so a reviewed Ignition change replaces it (`user_data` forces a new server). Reboots already end live messages, so a replacement in the update window costs what an OS update costs.
- **No SSH.** `sshd` masked and port 22 closed in both firewalls. Break-glass: Hetzner rescue through the API, or a re-provision with short-lived SSH certificates from an offline CA.
- **One Go host agent.** `cmd/nylzo-host`, a static binary shipped and verified like every release artifact. It verifies signatures and provenance in-process with `sigstore-go` (stable since v1.4.0, the library behind gh and cosign) and replaces the bash scripts, cosign, gh, jq and the tool channel. Subcommands: `update`, `activate`, `health`, `policy`, `status`, `clock`, `heartbeat`, `firewall`, `bandwidth`, `storage`, `clean`, `directive`. systemd keeps retries and conditions (`Restart=`, `RestartSteps=`, `ExecCondition=`).
- **Tests.** Table tests for the release policy, recorded release bundles as verification vectors, file effects in temporary directories, systemd behind an interface; the QEMU acceptance test stays.

## Migration that never breaks production

1. Build the agent to parity and ship it next to the bash bundle; VM acceptance runs the agent.
2. Move OpenTofu state to the bucket and enable `infrastructure.yml` (done in code; the owner moves the state).
3. Create a green server with the new module: new volume, new primary IPs, `publicAccess: false`. Add its addresses to the Tang allowlists and run the acceptance workflow.
4. Allow the green Caddy's ACME account in CAA, lower the DNS TTL and switch the records through the DNS stack. Watch the production watch and the browser check.
5. Keep the old server for a rollback window, then remove its protection in a reviewed pull request and destroy it.
6. Delete the bash scripts, the tool channel, the SSH configuration and the container stub tests.

## Stays manual

- Account two-factor authentication, Hetzner token rotation (the API has no token endpoints), registrar locks.
- Offline custody of the reporting key.
- Approving the protected `infrastructure` environment, if the owner keeps a required reviewer.
