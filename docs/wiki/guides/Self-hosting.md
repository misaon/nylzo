# Self-hosting

This guide is for people who run their own Nylzo instance with Docker Compose: the first start, the settings you change, maintenance mode, reboots and upgrades. [Operations](Operations.md) has the configuration reference, keys, monitoring and troubleshooting.

The example in [deploy/compose](../../../deploy/compose) runs two containers: `nylzo`, the server, and `caddy`, a Caddy build from the same Nylzo release that terminates TLS and serves the web client. Whoever controls the TLS endpoint can replace the web client, so run it on a host you trust as much as the server ([security model](Security-model.md)).

## Requirements

- Linux with Docker Engine 28.0 or newer, Docker Compose v2 and Docker's default IPv6 firewall support. Docker Desktop can merge client addresses, which breaks per-address rate limits.
- Docker's data root on ext4 or XFS. Nylzo erases a deleted message by overwriting its blocks in place and refuses btrfs, ZFS and overlay storage.
- No active swap, zram included, unless the epoch directory gets a `noswap` tmpfs (below).
- systemd, whose tmpfiles entry creates the epoch directory at every boot.
- A domain pointing to the host, and ports 80/tcp, 443/tcp and 443/udp open.
- Disk for eight days of your daily cap. A message holds up to about 5.4 MB, so the default 2,000 messages a day need about 86 GB.

## Prepare the host

1. [Verify the release](Verify-a-release.md). Verification prints digest-pinned references for both images.
2. From [deploy/compose](../../../deploy/compose) of that release, copy `compose.yaml`, `Caddyfile`, `check-isolation`, `nylzo-epoch.conf` and `.env.example` into a private directory. Copy `.env.example` to `.env`.
3. Fill in `.env`:

| Variable | Value |
| --- | --- |
| `NYLZO_DOMAIN` | Your host name in lowercase |
| `NYLZO_IMAGE`, `NYLZO_CADDY_IMAGE` | The references from step 1 |
| `NYLZO_GLOBAL_DAILY_CREATES` | Messages a day your disk can hold, default 2000 |
| `NYLZO_OPERATOR`, `NYLZO_CONTACT`, `NYLZO_HOSTING` | Who runs the instance, one e-mail or `https://` contact, and where it runs, shown on the transparency page |

Compose sets `NYLZO_PUBLIC_URL` from the domain. Self-hosted instances tell search engines not to index their pages; only nylzo.com is indexable.

Run the rest in the deployment directory. Generate the pepper, a 32-byte server secret that only UID 65532, the container's user, may read:

```sh
umask 077
mkdir -p secrets
openssl rand -hex 32 > secrets/pepper
sudo chown 65532:65532 secrets/pepper
sudo chmod 0400 secrets/pepper
```

Create the directory for Caddy's certificates and ACME account. Caddy runs as UID 65533. Neither UID may belong to an account on the host.

```sh
sudo install -d -o 65533 -g 65533 -m 0700 caddy-state
```

Install the tmpfiles entry. It creates `/run/nylzo/epoch` on the host's tmpfs for the epoch seed, the key material that makes a message unreadable after its deadline:

```sh
sudo cp nylzo-epoch.conf /etc/tmpfiles.d/
sudo systemd-tmpfiles --create nylzo-epoch.conf
```

Turn swap off for good, so the kernel never writes the seed to disk. Nylzo refuses a tmpfs while swap is active.

```sh
sudo swapoff -a && sudo sed -i.bak '/\sswap\s/s/^/#/' /etc/fstab
```

Turn off zram and systemd swap units in their own configuration too, then check that `swapon --show` prints nothing. A host that needs swap can instead mount a `noswap` tmpfs (Linux 6.4 or later) on `/run/nylzo/epoch`, keep the tmpfiles entry, and mask hibernation with `sudo systemctl mask hibernate.target hybrid-sleep.target suspend-then-hibernate.target`:

```text
tmpfs /run/nylzo/epoch tmpfs noswap,size=1m,mode=0700,uid=65532,gid=65532,nosuid,nodev,noexec,nofail 0 0
```

## Start

```sh
docker compose up -d
sudo bash check-isolation
```

`check-isolation` confirms that Nylzo can reach Caddy and nothing else. It fails on Docker Engine before 28.0, a host address on the `backend` network, reverse-path filtering turned off (set `net.ipv4.conf.all.rp_filter = 2` in `/etc/sysctl.d/`), a third container on `backend`, a default route in Nylzo, or a Caddyfile that proxies elsewhere. Run it after every Docker or Nylzo upgrade.

Then open `https://your-domain/transparency` and run `nylzo verify-client --server https://your-domain` with the [CLI](CLI.md) of the same release.

## What the example sets up

- Both containers have a read-only root filesystem, no capabilities, `no-new-privileges`, resource limits and no core dumps. Nylzo writes only to the `nylzo-data` volume and the epoch directory, and gets the pepper as a Docker secret file.
- Nylzo is attached only to `backend`, an internal network with no route out. Caddy is also attached to `edge`, where Docker publishes its ports. Nylzo trusts forwarding headers only from Caddy's fixed addresses. If the subnets clash with your network, change them, both fixed addresses, the Caddyfile upstream and `NYLZO_TRUSTED_PROXIES` together.
- Caddy accepts only TLS 1.3 with X25519MLKEM768, a hybrid post-quantum key exchange, so older browsers and many bots cannot connect ([transport](Security-model.md#transport)).
- Caddy keeps no access log. Add no `log` directive to a site and keep the `exclude` list: a logged `X-Nylzo-Lease` header lets anyone with the link download a message without spending an open. Caddy started with `--adapter nylzo`, as `compose.yaml` does, refuses such a Caddyfile.

Other server settings go into the `environment` block of the `nylzo` service ([configuration](Operations.md#configuration)); apply them with `docker compose up -d`.

## Server commands

```sh
docker compose exec -T nylzo /ko-app/nylzo takedown < approved-ids.txt
docker compose exec -T nylzo /ko-app/nylzo admin-enroll
```

`takedown` removes the messages whose ids stand one per line in the file. `admin-enroll` prints a link that adds a passkey to the admin area at `/admin`; open it within 15 minutes. [Operations](Operations.md#operator-commands) lists every command.

Do not use `docker compose run nylzo`, whose container takes Nylzo's fixed address. Never copy files into the container with `docker cp`: Docker 28 and older let a compromised container redirect such a copy onto host files.

## Maintenance mode

`sudo touch caddy-state/maintenance` starts maintenance and `sudo rm caddy-state/maintenance` ends it, without a restart. While the file exists, pages get a maintenance page with status 503 and `Retry-After: 30`, API and transparency requests get 503 `{"error":"unavailable"}` without reaching Nylzo, and assets and `/healthz` pass as usual. Messages keep expiring on schedule.

## Reboots

The epoch seed exists only in memory, so a host reboot ends every live message. Nylzo then creates a new seed and logs `epoch-seed-created`, and the first request about an older message ends it as `lost` and erases its blob. Restarting containers or Docker, `docker compose down`, and upgrades keep the seed.

- Reboot only when an update needs it, such as a new kernel, at a quiet time. With unattended upgrades, leave `Unattended-Upgrade::Automatic-Reboot` off or set `Unattended-Upgrade::Automatic-Reboot-Time` to your window.
- After a reboot, check that `swapon --show` prints nothing and that `timedatectl` reports a synchronised clock.
- An unplanned `epoch-seed-created` in `docker compose logs nylzo` means that the host restarted or someone emptied `/run/nylzo/epoch`.

Never copy the seed to save messages: a copy, a link and a stored message together open that message at any later time.

## Upgrade

Take both images, `compose.yaml`, `Caddyfile`, `check-isolation` and `nylzo-epoch.conf` from the same release. If you serve another host name from the same site block, such as `www`, add it to `nylzo_crawler_tls`.

1. Verify the new release and put its references into `.env`. Leave `secrets/pepper` as it is.
2. Ask the new image whether it can open your data. The check writes nothing and exits with 1 where the server would refuse to start.

```sh
image=$(sed -n 's/^NYLZO_IMAGE=//p' .env)
docker run --rm --user 65532:65532 --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true --ulimit memlock=-1:-1 --ulimit core=0:0 \
  --env NYLZO_DATA_DIR=/home/nonroot --env NYLZO_EPOCH_DIR=/run/nylzo-epoch \
  --volume nylzo_nylzo-data:/home/nonroot:ro --volume /run/nylzo/epoch:/run/nylzo-epoch:ro \
  "$image" store-check
```

3. Run `docker compose up -d`, then `sudo bash check-isolation`.

Never go back to an older release once a newer one has started on your data.

### From 0.13 to 0.14

The first start of 0.14.0 erases every stored message, the 0.13 seed and any stored abuse reports, so every earlier link stops working. CLIs before 0.14.0 get `client_outdated`. Copying the old seed keeps nothing, since 0.14.0 erases a 0.13 seed wherever it finds one, and it refuses to start while the old `epoch` directory is still in `nylzo-data`.

1. Tell your users when open messages will be deleted and that they need CLI 0.14.0 or later.
2. Install Docker Engine 28.0 or newer, the tmpfiles entry and the swap settings from [Prepare the host](#prepare-the-host). Verify the 0.14 images.
3. Turn on maintenance mode and stop Nylzo: `sudo touch caddy-state/maintenance && docker compose stop nylzo`.
4. Erase the old seed directory in place, since `rm` or `mv` would leave its blocks on the disk:

```sh
data=$(docker volume inspect --format '{{.Mountpoint}}' nylzo_nylzo-data)
sudo find "$data/epoch" -type f -exec shred --zero --iterations=0 --remove=unlink {} +
sudo rmdir "$data/epoch"
```

5. Copy in the new release's files, put its references into `.env` and delete `NYLZO_REPORT_KEY`, which 0.14 ignores, and `NYLZO_PROXY_MODE`, which stops 0.14 from starting.
6. Run the `store-check` above. It should report that the server upgrades the store and erases every message, and that there is no seed.
7. Run `docker compose up -d` and `docker network rm nylzo_nylzo` for the old network, then `sudo bash check-isolation` and `sudo rm caddy-state/maintenance`.

The log then shows `store-upgraded` and `epoch-seed-created`, plus `reports-erased` if 0.13 stored abuse reports. Delete any backup or snapshot of `nylzo-data` from before the upgrade: it holds the old seed and messages.
