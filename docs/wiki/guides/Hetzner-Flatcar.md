# Hetzner Cloud with Flatcar

This runbook is for whoever runs the hardened production profile in [deploy/hetzner-flatcar](../../../deploy/hetzner-flatcar), as nylzo.com does: one Hetzner Cloud server on Flatcar Container Linux, with short restart windows and no failover. It covers provisioning, updates, host settings and recovery. [Operations](Operations.md) has the server settings, monitoring and troubleshooting.

## What the profile sets up

- Three OpenTofu stacks with encrypted state and plans: `image` builds a verified Flatcar snapshot, `server` creates the server and its cloud firewall, `dns` manages a Cloudflare zone without proxying.
- Flatcar Stable updates itself and reboots between 03:00 and 04:00 UTC, which ends every live message. A new OS version that fails Nylzo's health check reboots back to the previous one.
- Nylzo runs in Docker with user namespace remapping and SELinux enforcing, 1 GiB of memory, `GOMEMLIMIT=512MiB` and no route out. Caddy runs on the host and serves the web client from its own binary.
- The cloud firewall and an nftables policy on the host each admit only 80/tcp, 443/tcp and 443/udp, ICMP, SSH from the administrator networks, and 8443/tcp when the [host monitor](Operations.md#hetzner-host-readiness) is on. Each IPv4 address or IPv6 /64 may hold 64 connections to the web ports, and outgoing traffic is capped at 40 Mbit/s.
- Caddy accepts only TLS 1.3 with X25519MLKEM768 ([transport](Security-model.md#transport)), gets Let's Encrypt `shortlived` certificates over HTTP-01 and writes no access log.
- SSH admits only user `core` with a hardware-backed key and a touch on every login.
- With the `strict` profile, data, pepper, Caddy's keys and the journal live on an [encrypted volume](#encrypted-data-volume) that opens only while your Tang servers answer.
- The epoch seed lives in `/run/nylzo-epoch` on tmpfs, and host policy refuses swap. Nylzo starts only after `systemd-timesyncd` has synchronised the clock ([expiry shares](../reference/Protocol.md#56-expiry-shares)); until then the host report shows `services` as false.
- Hetzner backups are off, because a restored snapshot would bring back messages that were already read or deleted.

Hetzner and your administrator accounts stay trusted, and root on the host can serve altered browser code.

## Before you start

- Create a dedicated Hetzner Cloud project. A Read & Write API token controls the server, so pass it to one command at a time, as `HCLOUD_TOKEN` from a password manager or a private file named in `HCLOUD_TOKEN_FILE`.
- Turn on the strongest 2FA at Hetzner, the DNS provider, the registrar and GitHub.
- Pick administrator networks: IPv4 /24 or narrower, IPv6 /64 or narrower.
- Create a hardware-backed SSH key. With a FIDO2 security key: `ssh-keygen -t ed25519-sk -O verify-required -O resident`. On a Mac with Touch ID, `sc_auth create-ctk-identity -l nylzo-admin -k p-256-ne -t bio` creates a Secure Enclave key and `ssh-keygen -w /usr/lib/ssh-keychain.dylib -K -N ""` writes its key files. Check for `bio` in the Prot column of `sc_auth list-ctk-identities`; a key created with `-t none` signs without asking.
- Install [mise](https://mise.jdx.dev/getting-started.html), check out a reviewed revision and [verify](Verify-a-release.md) your `nylzo` CLI.
- For the strict profile, run two to five Tang servers at other providers. Firewall them to the server, log every request and alert on requests outside the reboot window.

## Configure the site

```sh
umask 077
mkdir -p .deploy
mise run deploy init "$HOME/.ssh/id_ed25519_sk.pub" .deploy/production
```

`init` writes `state-encryption.key` and `site.json`. Back the directory up away from the server, because without the key the OpenTofu state cannot be read. Keep `site.json`, which names your administrator networks, out of the repository.

| Field | Value |
| --- | --- |
| `domain`, `dnsZone` | Host name and its zone; when equal, the apex gets a `www` redirect |
| `location` | `nbg1`, `fsn1` or `hel1` |
| `serverType`, `architecture` | `cax` types are `arm64`, the others `amd64` |
| `operator`, `contact`, `hosting` | Transparency page; operator and contact are required |
| `dailyCreates` | `NYLZO_GLOBAL_DAILY_CREATES`, at most 500 |
| `initialRelease` | Release a new server starts with; planning checks its signature, Caddy build and host bundle |
| `automaticUpdates` | `true` installs signed releases on its own |
| `profile`, `tang` | `"strict"` requires `tang`: a `threshold` and two to five servers with `url` and `thumbprint` |
| `publicAccess` | `false` lets only the administrator networks reach HTTPS |
| `heartbeatUrl`, `monitorClientCertificate` | Optional [host monitoring](Operations.md#hetzner-host-readiness) |

Read a Tang thumbprint with `jose jwk thp -a S256 -i <signing key>.jwk`.

## Provision

```sh
mise run deploy plan image .deploy/production/site.json
mise run deploy apply image .deploy/production/site.json
mise run deploy plan server .deploy/production/site.json
mise run deploy apply server .deploy/production/site.json
```

Read each plan before applying it. `apply` takes only the saved plan, with unchanged inputs and under an hour old.

The server's Ignition downloads the host bundle of `initialRelease` and checks its SHA-256 before the first boot. OpenTofu ignores later changes to Ignition and the image, so a running server changes only through signed releases and the host steps below.

Before the first SSH login, compare the host key fingerprint on the Hetzner console with the one SSH offers. Before you accept real traffic:

1. Check DNSSEC, the CAA records, certificate issuance, the `www` redirect, IPv4 and IPv6, and that SSH and the backend refuse other sources.
2. Run `nylzo verify-client --server https://your-domain` with the installed release's CLI, and create, read and burn test messages.
3. Test the heartbeat alert.

To open the service, set `publicAccess: true` and apply the server stack. Then set `PUBLIC_ACCESS=true` in `/etc/nylzo/host.env` and run `sudo systemctl restart nylzo-firewall`. Both firewalls must agree, also on `ADMINISTRATOR_NETWORKS`.

## DNS with Cloudflare

Create the zone in Cloudflare. The stack imports it with the records it manages and stops when a managed kind of record exists twice. Give the API token Zone Read, DNS Edit and SSL and Certificates Edit for this zone only, and pass it as `CLOUDFLARE_API_TOKEN` or `CLOUDFLARE_API_TOKEN_FILE`. `site.json` needs `cloudflareAccountId`.

```sh
mise run deploy plan dns .deploy/production/site.json
mise run deploy apply dns .deploy/production/site.json
mise run deploy outputs dns .deploy/production/site.json
```

The stack manages unproxied A and AAAA records, DNSSEC, a `www` CNAME for an apex domain, a null MX, `v=spf1 -all`, a DMARC reject policy, CAA `issue "letsencrypt.org; validationmethods=http-01"` with `issuewild ";"`, and Universal SSL turned off. Then:

1. Delegate to the printed name servers and publish the printed DS record at the registrar.
2. After Caddy's first certificate, set `acmeAccountUri` to its ACME account URL and apply again:

```sh
sudo /opt/nylzo/bin/jq -r .location /var/lib/caddy/caddy/acme/acme-v02.api.letsencrypt.org-directory/users/*/*.json
```

3. Check that `dig CAA your-domain +dnssec` shows exactly two CAA records.
4. At the registrar, turn on the transfer, update and delete locks and auto-renew.

## Infrastructure from GitHub Actions

In misaon/nylzo, `infrastructure.yml` plans and applies the stacks from `main`. A push that changes a stack, the toolchain or the deploy tool plans in the `infrastructure-plan` environment and applies plans with changes in `infrastructure`. A daily run only plans and fails on drift.

One-time setup:

1. Create a private S3-compatible bucket that supports conditional writes, which the state lock needs, and an access key for it only.
2. With `NYLZO_STATE_BUCKET`, `NYLZO_STATE_ENDPOINT`, `NYLZO_STATE_REGION`, `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` set, run `mise run deploy migrate server .deploy/production/site.json` and the same for `dns`. A new plan must show no changes.
3. Create the environments `infrastructure-plan` and `infrastructure`, limited to `main`. Store in both the secrets `NYLZO_SITE_JSON` (`site.json`), `NYLZO_STATE_PASSPHRASE` (`state-encryption.key`), `NYLZO_STATE_ENDPOINT`, `NYLZO_STATE_ACCESS_KEY` and `NYLZO_STATE_SECRET_KEY`, plus `HCLOUD_TOKEN` and `CLOUDFLARE_API_TOKEN`: read-only in `infrastructure-plan`, Read & Write in `infrastructure`.
4. Set the repository variables `NYLZO_STATE_BUCKET`, `NYLZO_STATE_REGION` and `NYLZO_INFRASTRUCTURE_ENABLED=true`.
5. Run `gh workflow run infrastructure.yml -f stack=image`, then `stack=server` and `stack=dns`.

## Updates

With `automaticUpdates: true`, `nylzo-update.timer` looks for a release every ten minutes.

- A newer patch of the installed line installs directly. A new minor or major line installs only when its `vX.Y.0` carries a signed `planned` approval ([Releasing](../maintainers/Releasing.md)), and then the host takes that line's newest patch. Downgrades are refused.
- The release must be immutable, signed by the release workflow for its tag and have GitHub provenance. The updater runs the image on empty data without network, then runs its `store-check` on the live data, mounted read-only. Any failure leaves the running release as it is.
- It installs the release's Caddy and host bundle `nylzo-host_X.Y.Z.tar` (every host script, unit and configuration file), restarts only what changed, and goes back to the previous bundle if host policy or Caddy fails. Last, it stops Nylzo and starts the new image.
- If the new release fails its health check on the live data, the updater stops Nylzo, writes `/var/lib/nylzo-deploy/failed.json` and installs nothing more until you [recover](#recovery). It never restores an old database.

```sh
sudo systemctl status nylzo caddy nylzo-firewall nylzo-update.timer update-engine
sudo journalctl -u nylzo-initialize -u nylzo-update -u nylzo-ready -u nylzo
sudo cat /var/lib/nylzo-deploy/current.json; readlink /opt/nylzo/host/current
```

### Upgrade to 0.14.0

The first start of 0.14.0 erases every stored message, the 0.13 seed and any stored abuse reports, so every earlier link stops working, and CLIs before 0.14.0 get `client_outdated`. The updater installs it once `v0.14.0` is approved as planned.

1. Tell your users when open messages will be deleted and that they need CLI 0.14.0 or later.
2. Make sure `/etc/nylzo/service.env` contains `NYLZO_EDGE_REPORTS=1`, which servers provisioned before the admin area lack. Any `NYLZO_REPORT_KEY` line there is ignored.
3. After the upgrade, look for `store-upgraded` and `epoch-seed-created` in `sudo journalctl -u nylzo`.
4. Set `initialRelease` to the new tag in `site.json` and `NYLZO_SITE_JSON`, and drop `reportPublicKey`.

## Host settings

| File in `/etc/nylzo` | Holds |
| --- | --- |
| `host.env` | `AUTOMATIC_UPDATES`, `PUBLIC_ACCESS`, `ADMINISTRATOR_NETWORKS`, `HARDWARE_KEYS_ONLY`, `ENCRYPTED_STORAGE_REQUIRED`, `WWW_REDIRECT` |
| `service.env` | Nylzo's environment ([configuration](Operations.md#configuration)) |
| `caddy.env` | Domain and contact for Caddy; activation copies the contact from `service.env` |
| `heartbeat.url`, `monitor-client.pem` | Host monitoring |
| `tang.json` | Tang servers |

After changing `host.env`, `heartbeat.url`, `monitor-client.pem` or the contact, re-apply the current host bundle. After any change to `service.env`, also restart Nylzo:

```sh
sudo /opt/nylzo/runtime/activate "$(readlink -e /opt/nylzo/host/current)"
sudo systemctl restart nylzo
```

Activation refuses `HARDWARE_KEYS_ONLY=true` while `core` has no hardware-backed key, and `ENCRYPTED_STORAGE_REQUIRED=true` without `tang.json`.

Flatcar rebuilds `~core/.ssh/authorized_keys` from `~core/.ssh/authorized_keys.d` at every boot, so manage keys as `core` with `update-ssh-keys`: `-a NAME FILE.pub` adds a key set, `-l` lists them, `-d NAME` removes one. Keep a second hardware key enrolled, since the console has no login.

## Maintenance mode

The flag is `/var/lib/nylzo-directive/maintenance`, and Caddy treats it as [Self-hosting](Self-hosting.md#maintenance-mode) describes. For nylzo.com, publish a signed directive instead:

```sh
gh workflow run watch.yml -f directive=maintenance
gh workflow run watch.yml -f directive=normal
```

The host checks every minute and applies only a newer directive for its own domain, which overrides a flag set by hand. Other instances use `sudo touch` and `sudo rm` on the flag.

## Production watch

For nylzo.com, `watch.yml` checks about every 30 minutes that production serves the web client of its signed release. A confirmed difference, or two failed runs in a row, opens a `production-incident` issue. A confirmed difference also publishes a maintenance directive and, with `NYLZO_KILL_SWITCH_ENABLED=true`, cuts the server's cloud firewall to administrator SSH, which a compromised server cannot undo.

- Create the environment `production-watch` with a 27-minute wait timer, limited to `main`, then set the variable `NYLZO_WATCH_CHAIN=true`.
- For the kill switch, create the environment `kill-switch` limited to `main`, without a required reviewer, which would hold later runs. Store the secrets `HCLOUD_TOKEN` (Read & Write) and `NYLZO_ADMINISTRATOR_NETWORKS`, and set the repository variables `NYLZO_FIREWALL_ID` and `NYLZO_KILL_SWITCH_ENABLED=true`.
- Turn on GitHub notifications for assigned issues.

## Pepper rotation

`prepare` writes a new pepper whose `pepper_id` differs from the previous one, and `nylzo.service` passes `pepper.previous` to Nylzo while it exists:

```sh
sudo mv /var/lib/nylzo-secrets/pepper /var/lib/nylzo-secrets/pepper.previous
sudo /opt/nylzo/runtime/prepare
sudo systemctl restart nylzo
```

After seven days, delete `pepper.previous` and restart Nylzo. Deleting `pepper` without keeping it as `pepper.previous` closes every stored message.

## Encrypted data volume

With `tang` set, `nylzo-storage.service` keeps `/var/lib/nylzo`, `/var/lib/nylzo-secrets`, `/var/lib/caddy` and `/var/log/journal` on a LUKS2 volume in `/var/lib/nylzo-storage.luks`, which Clevis binds to your Tang servers. When the volume does not open, Nylzo, Caddy and the updater stay stopped, and a host with the volume file never falls back to plain directories. The volume takes the root filesystem's free space minus 8 GiB.

It protects against a copied disk, a leaked snapshot or backup, and a snapshot booted elsewhere. It does not protect against a compromised running host, or a token holder who boots the server into rescue mode while Tang answers.

After the first boot, add an offline recovery passphrase. Find the loop device with `losetup -j /var/lib/nylzo-storage.luks` and the Clevis slot with `sudo clevis luks list -d <device>`, then run `sudo clevis luks pass -d <device> -s <slot> | sudo cryptsetup luksAddKey --key-file - <device>` and store the new passphrase offline.

### Move an existing server onto the volume

This needs host bundle 0.14.0 or later, and `df /var/lib` must show the size of the three data directories plus 17 GiB free. Work outside the reboot window with the Hetzner console open.

1. Write `/etc/systemd/system/nylzo-storage.service.d/50-migration.conf` with the lines `[Unit]` and `AssertPathExists=/nonexistent`, so nothing that needs the volume starts. Run `sudo systemctl daemon-reload` and `sudo systemctl stop nylzo-update.timer nylzo-host-status.timer`, and wait until `systemctl show --property=ActiveState --value nylzo-update.service` no longer prints `activating`. Run `sudo systemctl stop nylzo-storage.service nylzo.service caddy.service` and check that `systemctl is-active nylzo.service caddy.service` prints `inactive` twice.
2. Move `/var/lib/nylzo`, `/var/lib/nylzo-secrets` and `/var/lib/caddy` aside, for example to `/var/lib/nylzo.plain`, and create an empty mode-700 directory in each place. The volume step empties `/var/log/journal`, so copy it first if you need old entries.
3. Write `/etc/nylzo/tang.json` with mode 600, such as `{"t":2,"pins":{"tang":[{"url":"http://tang-a.example.net","thp":"<thumbprint>"},{"url":"http://tang-b.example.org","thp":"<thumbprint>"}]}}`, and make sure `host.env` has exactly one line `ENCRYPTED_STORAGE_REQUIRED=true`.
4. Run `sudo /opt/nylzo/runtime/storage`. `findmnt /var/lib/nylzo` must show `/dev/mapper/nylzo-storage[/nylzo]`. If the step fails, fix the cause and run it again.
5. Copy each directory back, such as `sudo cp -a /var/lib/nylzo.plain/. /var/lib/nylzo/`, compare with `sudo diff -r`, and add the recovery passphrase. Delete the drop-in, run `sudo systemctl daemon-reload` and reboot. Delete the plain copies once `systemctl is-active nylzo-ready.service nylzo.service caddy.service` reports all three active.

To give up before the reboot, keep the drop-in. If step 4 opened the volume, run `sudo journalctl --relinquish-var`, unmount `/var/log/journal`, `/var/lib/caddy`, `/var/lib/nylzo-secrets`, `/var/lib/nylzo` and `/var/lib/nylzo-storage` in that order, and run `sudo cryptsetup close nylzo-storage`. Detach the loop device of `/var/lib/nylzo-storage.luks` (or `.new`) and delete the file. Delete `tang.json`, set `ENCRYPTED_STORAGE_REQUIRED=false`, `rmdir` the empty directories, move each plain directory back with `mv -T`, delete the drop-in and reboot.

Afterwards add `"profile": "strict"` and `tang` to `site.json` and `NYLZO_SITE_JSON`. Free blocks of the old disk may still hold earlier data.

## Recovery

- Failed update: read `sudo journalctl -u nylzo -u nylzo-update`. If the release refused the data without changing it, run `sudo cp /var/lib/nylzo-deploy/previous.json /var/lib/nylzo-deploy/current.json`, `sudo rm /var/lib/nylzo-deploy/failed.json` and `sudo systemctl start nylzo`. If it already changed the data (`store-upgraded`, `rows-sealed`), older releases cannot open it; delete `failed.json` once a fixed patch is out and the updater installs it.
- Kill switch: after the investigation, run `gh workflow run infrastructure.yml -f stack=server -f restore=true` and `gh workflow run watch.yml -f directive=normal`, then close the issue. Server applies stop while the firewall holds only kill-switch rules.
- Suspected copy of the disk or a snapshot: deactivate the ACME account, revoke the certificate, [rotate the pepper](#pepper-rotation) and rotate the Tang keys.
