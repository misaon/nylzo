# Operations

This reference helps whoever runs a Nylzo server configure, monitor and troubleshoot it. For a first deployment start with [Self-hosting](Self-hosting.md) or [Hetzner and Flatcar](Hetzner-Flatcar.md).

## What the server needs

`nylzo serve` refuses to start unless all of these hold:

- It runs on Linux from a build with `GOEXPERIMENT=runtimesecret` for linux/amd64 or linux/arm64, which erases request keys from memory. Release builds are.
- The data and epoch directories lie on ext4 without `data=journal`, XFS or tmpfs, where an overwrite lands in place.
- It can lock its memory: `LimitMEMLOCK=infinity` in systemd, `--ulimit memlock=-1:-1` in Docker, `ulimits: memlock: -1` in Compose, or `CAP_IPC_LOCK` outside a user namespace.
- `NYLZO_PEPPER_FILE` is set, and a directory named in `NYLZO_EPOCH_DIR` exists.
- No swap is active while the epoch directory is on a tmpfs, unless the tmpfs has `noswap`.

A systemd unit also needs `LimitCORE=0`. The server speaks plain HTTP: put a TLS proxy in front and name it in `NYLZO_TRUSTED_PROXIES`.

## Configuration

A source is an IPv4 address or IPv6 /64, a range an IPv4 /24 or IPv6 /48. Durations take seconds or values such as `24h`. See [config.go](../../../internal/server/config/config.go).

| Variable | Default | Meaning |
| --- | --- | --- |
| `NYLZO_PEPPER_FILE` | required | Pepper file, 32 bytes in hex or base64url, mode 600 or 400 |
| `NYLZO_PEPPER_PREVIOUS_FILE` | unset | Previous pepper during a [rotation](#rotate-the-pepper) |
| `NYLZO_LISTEN` | `127.0.0.1:8080` | Listen address; without it, a platform's `PORT` means `:PORT` |
| `NYLZO_DATA_DIR` | `data` | `nylzo.db`, `admin.db` and `blobs/`; use an absolute path |
| `NYLZO_EPOCH_DIR` | `$NYLZO_DATA_DIR/epoch` | Epoch seed; put it on a tmpfs |
| `NYLZO_PUBLIC_URL` | unset | Public origin such as `https://nylzo.example` |
| `NYLZO_TRUSTED_PROXIES` | unset | Proxy addresses or CIDRs allowed to set `X-Forwarded-For` |
| `NYLZO_EDGE_REPORTS` | off | `1` only behind Nylzo's Caddy, which reports visits |
| `NYLZO_OPERATOR`, `NYLZO_CONTACT`, `NYLZO_HOSTING` | unset | Transparency page, up to 200 bytes each; contact is one e-mail or `https://` address, the same for Caddy |
| `NYLZO_GLOBAL_DAILY_CREATES` | `0` (off) | Messages per UTC day. Past half the cap proof of work rises, and at the cap creation answers 503. A range may take a twentieth, an IPv4 /16 or IPv6 /32 a fifth |
| `NYLZO_MAX_TTL`, `NYLZO_DEFAULT_TTL` | `168h`, `24h` | Longest and default lifetime, 60 seconds to seven days |
| `NYLZO_MAX_OPENS` | `10` | Opens per message, up to 100 |
| `NYLZO_MAX_FILE_BYTES` | `5242880` | Attachment limit; can only go lower |
| `NYLZO_RATE_CREATE_HOUR`, `NYLZO_RATE_CREATE_DAY`, `NYLZO_RATE_CREATE_BYTES_DAY` | `20`, `100`, `50000000` | Creations per source; `0` turns one off |
| `NYLZO_RATE_READ_HOUR`, `NYLZO_RATE_READ_BYTES_HOUR` | `300`, `268435456` | Message requests and bytes read per source and hour |
| `NYLZO_RATE_COARSE_MULTIPLIER` | `5` | Range limits as a multiple of source limits; `0` turns them off |
| `NYLZO_RATE_SOURCE_RPS`, `NYLZO_RATE_SOURCE_BURST` | `10`, `20` | API requests per source |
| `NYLZO_RATE_BUCKET_RPS`, `NYLZO_RATE_BUCKET_BURST` | `500`, `1000` | API requests for the instance |
| `NYLZO_POW_DIFFICULTY`, `NYLZO_POW_CONCURRENCY` | `4`, `4` | Proof-of-work difficulty (0 to 8) and parallel checks (1 to 4, 16 MiB each) |
| `NYLZO_MAX_STREAMS` | `256` | Concurrent uploads and downloads, up to 1024 |
| `NYLZO_MIN_FREE_BYTES` | `1073741824` | Free disk to keep; below it creation stops |
| `NYLZO_SWEEP_INTERVAL` | `5m` | Cleanup interval; `0` leaves cleanup to `sweep-once` |
| `NYLZO_ALLOW_SWAP`, `NYLZO_ALLOW_UNERASABLE_STORAGE`, `NYLZO_ALLOW_UNERASED_MEMORY` | unset | `1` skips a start check, on development machines only |
| `GOMEMLIMIT` | `192MiB` | Go memory limit; set about half the container's memory |

## Operator commands

Run them on the server host with the same environment as `serve`.

| Command | What it does |
| --- | --- |
| `store-check` | Says, without writing, whether this build opens, upgrades or refuses the data; exits 1 where `serve` would refuse. Run it before every upgrade |
| `sweep-once` | One cleanup run for an external scheduler; upgrade it together with `serve` |
| `takedown` | Removes the messages whose ids it reads one per line from stdin; readers see them as deleted. A line that is not an id rejects the whole input |
| `admin-enroll`, `admin-passkeys`, `admin-reset` | Manage the [admin area](#admin-area) |

## Keys

The pepper is a 32-byte server secret that seals stored records and derives passphrase-check keys. Losing it closes every stored message. A leaked pepper plus a copy of the data allows offline passphrase guessing.

The epoch seed is the file `seed` in `NYLZO_EPOCH_DIR`. `serve` starts a generation from fresh entropy every hour and moves every generation forward each minute, so the seed needs no rotation. After a message's deadline, the server can no longer produce its key share. If the file is lost, `serve` starts a new one and every live message ends.

### Rotate the pepper

1. Keep the current pepper as the previous one and point `NYLZO_PEPPER_PREVIOUS_FILE` at it.
2. Write a new pepper to `NYLZO_PEPPER_FILE`. If `serve` says both share a `pepper_id`, generate it again.
3. Restart. `serve` reseals every record with the new pepper and logs `rows-sealed`.
4. After seven days, the longest a message lives, remove the previous pepper and restart.

In Compose, add a second secret, run `sudo mv secrets/pepper secrets/pepper.previous`, create a new pepper as in [Self-hosting](Self-hosting.md#prepare-the-host) and run `docker compose up -d`:

```yaml
services:
  nylzo:
    environment:
      NYLZO_PEPPER_PREVIOUS_FILE: /run/secrets/nylzo_pepper_previous
    secrets: [nylzo_pepper, nylzo_pepper_previous]
secrets:
  nylzo_pepper_previous:
    file: ./secrets/pepper.previous
```

## Logs

The server writes JSON lines to stderr with timestamps rounded to the minute, and no line per request. Every hour it writes one `requests` line per method, route and status with a count, and its error counts, which can lag up to two hours. It never logs message ids, paths, client addresses, `Authorization` or `X-Nylzo-*` headers, or bodies.

Keep every proxy, CDN and tracing agent in front of Nylzo from recording headers or paths. With the link, a logged `X-Nylzo-Lease` downloads a message without spending an open. `serve` refuses `GODEBUG=http2debug=1` or `=2`.

## Monitoring and alerts

- Probe `https://your-domain/healthz`; it answers during maintenance too.
- Run `nylzo verify-client --server https://your-domain` with the CLI of the running release to check the served web client against the signed release.
- Alert on low disk space and the events marked in [Troubleshooting](#troubleshooting).

### Hetzner host readiness

Every two minutes a Hetzner host writes a report of seven booleans and the time, with no message data, keys or logs: `hostPolicy` (SELinux, kernel, firewall, isolation, encrypted volume), `services` (all units active), `diskReserve` (8 GiB free), `applicationUpdates` (no failed update, a check within 30 minutes), `osUpdates` (update-engine checked within 24 hours), `clock` (two of GitHub, Sigstore and Let's Encrypt agree within a minute) and `certificate` (valid 36 more hours, issued for at most seven days).

The heartbeat is the primary alert. Every five minutes the host posts to a dead-man's switch, its URL when every check passes and `URL/fail` otherwise.

1. Create a check at a service such as [healthchecks.io](https://healthchecks.io/), period 5 minutes, grace 10 minutes.
2. Set `heartbeatUrl` in `site.json` before provisioning, or write it to `/etc/nylzo/heartbeat.url` (root, mode 600) and re-apply the host bundle.
3. Stop `nylzo-host-status.timer` for 15 minutes, confirm the alert, and start it again.

A second channel serves the report on port 8443 to one pinned client certificate. Create the identity with `mise exec go -- go run ./tools/deploy/monitor-identity .deploy/production/monitoring`. Put `client.pem` into `monitorClientCertificate` (or `/etc/nylzo/monitor-client.pem`, then re-apply), and keep `client-key.pem` private. In the GitHub environment `host-monitoring`, limited to `main`, store the secrets `NYLZO_MONITOR_CERT`, `NYLZO_MONITOR_KEY`, `NYLZO_HOST_MONITOR_URL` (`https://your-domain:8443/healthz`) and `NYLZO_HOST_MONITOR_ADDRESS`, then set the variable `NYLZO_HOST_MONITOR_ENABLED=true`. `monitor.yml` then reads the report about every 15 minutes and fails 30 days before the one-year identity expires.

When its variables are set, `monitor.yml` also runs a CLI and a Chrome test against `NYLZO_MONITOR_URL` every hour (about 24 test messages a day each) and checks `NYLZO_MONITOR_DOMAIN`: CAA over DNSSEC, mail records, registrar locks, expiry and Certificate Transparency. GitHub schedules are best effort, so rely on the heartbeat for timely alerts.

## Admin area

`/admin` shows visit and message counts, server load and the clients refused in the last hour, to passkey holders.

- `nylzo admin-enroll` prints a link that adds one passkey. Open it within 15 minutes in the browser that will hold the passkey; it works once. Without `NYLZO_PUBLIC_URL`, only a loopback listen address gives a working link.
- `nylzo admin-passkeys` lists the passkeys, up to 8. Enroll a second device.
- `nylzo admin-reset` removes every passkey and link and ends every session within a minute.

Counting runs only while a passkey exists. `admin.db` keeps hourly counts for 15 days and daily counts for 400 days, without addresses. Behind Nylzo's Caddy, set `NYLZO_EDGE_REPORTS=1` or robots and scripts stay at zero. Mention the counting in your privacy policy.

## Backups

Never back up, snapshot or replicate the data directory (`nylzo.db` with its `-wal` and `-shm` files, `admin.db`, `blobs/`) or the epoch seed. A restored data directory brings back messages that were read, burned or expired, and a copy of the seed opens messages after their deadline. Turn off provider backups of their disk.

Never open `nylzo.db` with another SQLite client while `serve` runs, and never point two servers at one data directory. Back up the deployment configuration, and on Hetzner the private operator directory. A pepper copy helps only while its data exists; keep it away from the server.

## Troubleshooting

| Log event or error | Meaning and fix |
| --- | --- |
| `epoch-seed-created` | New seed; live messages ended. Expected at the first start and after a reboot, otherwise alert |
| `epoch-seed-on-disk` | The seed is not on a tmpfs; backups hold it. Move `NYLZO_EPOCH_DIR` |
| `epoch-storage-not-erasable` | Swap came on. Turn it off (alert) |
| `epoch-clock-behind` | The clock is behind the seed; new messages answer 503 until it catches up. A forward jump ends messages whose deadline it skips. Fix time sync |
| `rows-unopenable` | No configured pepper opens some records. Check both pepper files (alert) |
| `registration-pepper-missing` | A message needs the previous pepper. Restore `NYLZO_PEPPER_PREVIOUS_FILE` |
| `blob-changed` | Something else wrote to `blobs/`, or the disk returns damaged data (alert) |
| `disk-full` | Below `NYLZO_MIN_FREE_BYTES`; creation stops, reading continues (alert) |
| `forwarded-header-ignored` | Forwarding headers came from outside `NYLZO_TRUSTED_PROXIES` |
| "the data directory still holds an epoch directory" | `NYLZO_EPOCH_DIR` names another directory while the data directory still has `epoch`. Stop `serve`. To keep live messages of this release, copy `epoch/seed` into the empty `NYLZO_EPOCH_DIR`. Then `shred --zero --iterations=0 --remove=unlink` every file in `epoch` and remove it. Upgrading from 0.13? Follow [Self-hosting](Self-hosting.md#from-013-to-014) |
| "the seed file is damaged" | Moving it aside starts a new seed and ends every live message |
