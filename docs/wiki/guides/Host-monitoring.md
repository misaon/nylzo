# Host monitoring

Every Hetzner/Flatcar host writes a coarse readiness report every two minutes. It holds seven booleans and the observation time, and no message data, capabilities, identifiers, peppers, keys, logs, versions or host inventory. Two channels carry it out:

- The primary channel is a heartbeat: the host posts to an independent dead-man's switch every five minutes. Only up or down leaves the host, and missing beats raise the alert, so it works when GitHub's schedule lags.
- The secondary channel is an mTLS listener: Caddy serves the report on TCP **8443** to one pinned client certificate. GitHub Actions reads it about every 15 minutes, when its best-effort schedule runs.

## Checks

- `hostPolicy`: SELinux enforcement, effective kernel settings, the host firewall with input policy drop, Docker isolation, disabled CRI and transfer plugins, the traffic ceiling, and the encrypted volume when the host requires it (`ENCRYPTED_STORAGE_REQUIRED=true`, Tang configured, or an existing volume file).
- `services`: Nylzo, Caddy, OS readiness, the host firewall, update-engine, locksmithd and the application update timer are each active. One active unit cannot hide another inactive, failed or missing unit.
- `diskReserve`: at least **8 GiB** available, before Nylzo's 4 GiB creation cutoff.
- `applicationUpdates`: no deployment-failure marker and a successful update check within 30 minutes, or one still running within 15 minutes.
- `osUpdates`: update-engine reports a known non-error state and checked within 24 hours. This does not prove that every upstream patch is installed.
- `clock`: within the last hour, at least two of GitHub, Sigstore and Let's Encrypt agreed with the system clock to within a minute over TLS.
- `certificate`: the served certificate for the domain is valid for at least 36 more hours and was issued for at most seven days. A longer certificate, for example one issued before the `shortlived` profile, fails the check.

## Heartbeat

1. Create a check at a dead-man's-switch service, for example [healthchecks.io](https://healthchecks.io/): period 5 minutes, grace 10 minutes, notifications to a channel you read. Healthchecks.io accepts `POST` to the check URL for success and to `URL/fail` for failure.
2. Set `heartbeatUrl` in `site.json` to the HTTPS check URL before provisioning. On an existing host, write it to `/etc/nylzo/heartbeat.url` (root, `0600`) and re-apply the host bundle with `sudo /opt/nylzo/runtime/activate "$(readlink -e /opt/nylzo/host/current)"`.
3. Stop `nylzo-host-status.timer` for 15 minutes and confirm that the alert arrives, then start it again.

The heartbeat service runs without capabilities and cannot read Caddy's, Nylzo's or the deployment's state directories. The monitoring service learns the server's address and when it is up.

## mTLS listener

Create the client identity in the private operator directory:

```sh
mise exec go -- go run ./tools/deploy/monitor-identity .deploy/production/monitoring
```

This creates an ECDSA P-256 client identity with the neutral subject `client`, valid for one year. Keep `client-key.pem` on the operator workstation and in the protected monitoring environment. Set `monitorClientCertificate` in `site.json` to the contents of `client.pem`; it adds the listener and the 8443 firewall rules on both firewalls, and requires automatic updates. On an existing host, install the public certificate as `/etc/nylzo/monitor-client.pem` and re-apply the host bundle; the activation composes the Caddyfile and opens the port.

Verify the listener locally without putting secrets in arguments or history:

```sh
URL=https://your-domain:8443/healthz \
MONITOR_CERT_FILE=.deploy/production/monitoring/client.pem \
MONITOR_KEY_FILE=.deploy/production/monitoring/client-key.pem \
bun tools/security/monitor-host.mts
```

`MONITOR_ADDRESS` can pin the connection to the server's address; TLS still verifies the hostname.

In the repository hosting `monitor.yml`, create an environment **host-monitoring** restricted to `main`, and store everything about the host there, so public logs show only whether the job passed:

```sh
for name in NYLZO_MONITOR_CERT:client.pem NYLZO_MONITOR_KEY:client-key.pem; do
  gh secret set "${name%%:*}" --repo YOUR_OWNER/YOUR_REPOSITORY --env host-monitoring < ".deploy/production/monitoring/${name#*:}"
done
printf '%s' 'https://your-domain:8443/healthz' | gh secret set NYLZO_HOST_MONITOR_URL --repo YOUR_OWNER/YOUR_REPOSITORY --env host-monitoring
printf '%s' '203.0.113.7' | gh secret set NYLZO_HOST_MONITOR_ADDRESS --repo YOUR_OWNER/YOUR_REPOSITORY --env host-monitoring
gh variable set NYLZO_HOST_MONITOR_ENABLED --repo YOUR_OWNER/YOUR_REPOSITORY --body true
```

Delete the old repository variables `NYLZO_HOST_MONITOR_URL` and `NYLZO_HOST_MONITOR_ADDRESS`. The client rejects stale or future observations, missing fields and unhealthy components, requires the listener to refuse a client without a certificate, and asks for rotation 30 days before the identity expires.

## Public checks

`monitor.yml` also runs hourly:

- With `NYLZO_MONITOR_URL`, the client check verifies the signed release the instance runs and creates, decrypts and consumes a five-minute synthetic message, about 24 creations a day. It prints only pass or fail. It runs the CLI, which a hostile server can recognise ([security model](Security-model.md#the-client-is-part-of-the-trust-boundary)). The browser job then has Google Chrome, in a window under xvfb rather than headless, create and read a message through the web client, and requires every asset Chrome receives to match the signed `checksums.txt`. The server sees an ordinary Chrome; only GitHub's address ranges set it apart. It costs another 24 creations a day. Both detect changes served to everyone, not changes aimed at one visitor.
- With `NYLZO_MONITOR_DOMAIN` (and optionally `NYLZO_ACME_ACCOUNT` and `NYLZO_CT_REVIEWED_UNTIL`), the domain check reads the CAA set over a DNSSEC-validating resolver, null MX, SPF and DMARC, the registrar locks and an expiry more than 180 days away, and every certificate issued in the last eight days for the domain or its subdomains in Certificate Transparency, which must come from Let's Encrypt for the apex or `www` only. Set `NYLZO_CT_REVIEWED_UNTIL` to an ISO date to acknowledge certificates you reviewed. Enable it after the DNS stack and registrar lock are applied.
- The maintenance watchdog fails when a merged security fix is still unpublished after an hour, or another merged update after four days.

## Production watch

`watch.yml` compares the production web client with its signed release about every 30 minutes. It runs the `verify-client` of the release the server claims, so exit 3 and the host's directive timer take effect with the first release that contains them; until then a changed client exits 1 and opens an incident after two runs.

- While a maintenance directive that `watch.yml` signed for this instance is in force, the 503 of the maintenance page is expected and opens nothing; a changed client still counts.
- A redirect, or a `client.json` that names no release, counts as a changed client. Otherwise it downloads the release the instance names, checks its signature, and runs `nylzo verify-client`. The release must be the latest one, or during the latest one's first day the release right before it. Exit 3 means a page, file, manifest or policy header differs, or the server answered with a redirect or an oversized response; the watch checks again two minutes later, because a release restarts Caddy for a moment. Exit 1 means the check could not finish; each check stops after four minutes.
- A confirmed difference opens one `production-incident` issue assigned to the owner at once (at most one comment an hour afterwards). Any other failure opens it when the previous run failed too, because a server can refuse the watch to hide a changed client. An outage longer than one watch interval therefore opens an issue as well.
- A confirmed difference also publishes a signed maintenance directive for the domain of `NYLZO_MONITOR_URL`, which must equal the host's `NYLZO_DOMAIN`, on the `production-directive` branch. The host checks it every minute (`nylzo-directive.timer`); GitHub serves the file from a cache for up to five minutes, so a new directive takes effect within about six. The host verifies the Sigstore signature of `watch.yml` on `main`, applies only a directive for its own `NYLZO_DOMAIN` that is newer than the last one, and shows the maintenance page. This helps when root on the host is intact; an attacker with root ignores it.
- With `NYLZO_KILL_SWITCH_ENABLED=true`, a confirmed difference also replaces the rules of the server's Hetzner Cloud Firewall with administrator SSH only. Any confirmed difference counts, including a misconfiguration such as a Caddy `NYLZO_CONTACT` that differs from the server's, so production stays closed until you restore it. A compromised server cannot undo that; visitors see a connection error, not the maintenance page, because HSTS preload allows no other host to answer for the name. Connections already open stay up. While the firewall holds only kill-switch rules, a server apply stops, so an automatic infrastructure update cannot reopen it. To restore after the investigation, run `gh workflow run infrastructure.yml -f stack=server -f restore=true`, publish `gh workflow run watch.yml -f directive=normal`, then close the issue.
- The watch is the only scheduler of Renovate and the dependency release windows, because GitHub drops many scheduled runs. Each run starts Renovate when six hours have passed since its last start, and the window when its turn on Monday, Wednesday or Friday has passed.
- It chains itself: each run starts the next. The `production-watch` environment's wait timer of 27 minutes spaces the runs about 30 minutes apart without holding a runner, and a cron at minutes 4 and 34 restarts the chain if it stops.

Owner setup:
- Create the `production-watch` environment with a wait timer of 27 minutes and deployment branch `main`, then set `NYLZO_WATCH_CHAIN=true`. Without the wait timer the chain would run back to back, so the variable stays unset until then.
- For the kill switch, create the `kill-switch` environment with deployment branch `main` and no required reviewer: a pending approval would hold every later watch run. Store a Hetzner Read & Write token as `HCLOUD_TOKEN` and the comma-separated administrator CIDRs as `NYLZO_ADMINISTRATOR_NETWORKS` in it, both as secrets, because the public log prints every variable a step receives. Then set `NYLZO_FIREWALL_ID` and `NYLZO_KILL_SWITCH_ENABLED=true` as repository variables, because the infrastructure workflow reads them too. A Hetzner token cannot be narrowed to one firewall; keep it only in that environment.
- Turn on GitHub notifications for assigned issues, so an incident reaches the GitHub app on your phone.

The domain monitor rejects redirects and API bodies larger than 1 MiB, including streamed responses without a declared length. It evaluates Certificate Transparency results one page at a time and requires an empty final page. If 50 pages do not finish the listing, the check fails as incomplete; review the backlog before changing that bound.

GitHub schedules are [best effort](https://docs.github.com/en/actions/how-tos/troubleshoot-workflows); rely on the heartbeat for timely alerts. Test notification delivery for each channel; a failed run alone does not prove that a message reached anyone.

References: [Caddy client authentication](https://caddyserver.com/docs/caddyfile/directives/tls#client_auth), [healthchecks.io HTTP API](https://healthchecks.io/docs/http_api/), [Cert Spotter API](https://sslmate.com/help/reference/ct_search_api_v1), [Flatcar update states](https://www.flatcar.org/docs/latest/updates-releases/releases/update-strategies/).
