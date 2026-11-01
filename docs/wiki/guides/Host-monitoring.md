# Host monitoring

Every Hetzner/Flatcar host writes a coarse readiness report every two minutes. It holds seven booleans and the observation time, and no message data, capabilities, identifiers, peppers, keys, logs, versions or host inventory. Two channels carry it out:

- **Heartbeat (primary).** The host posts to an independent dead-man's switch every five minutes. Only up or down leaves the host, and missing beats raise the alert, so it works when GitHub's schedule lags.
- **mTLS listener (secondary).** Caddy serves the report on TCP **8443** to one pinned client certificate. GitHub Actions reads it about every 15 minutes, when its best-effort schedule runs.

## Checks

- `hostPolicy`: SELinux enforcement, effective kernel settings, the host firewall with input policy drop, Docker isolation, disabled CRI and transfer plugins, the traffic ceiling, and the encrypted volume when Tang is configured.
- `services`: Nylzo, Caddy, OS readiness, the host firewall, update-engine and the update timer are active.
- `diskReserve`: at least **8 GiB** available, before Nylzo's 4 GiB creation cutoff.
- `applicationUpdates`: no deployment-failure marker and a successful update check within 30 minutes, or one still running within 15 minutes.
- `osUpdates`: update-engine reports a known non-error state and checked within 24 hours. This does not prove that every upstream patch is installed.
- `clock`: within the last hour, at least two of GitHub, Sigstore and Let's Encrypt agreed with the system clock to within a minute over TLS.
- `certificate`: the served certificate for the domain is valid for at least 36 more hours.

## Heartbeat

1. Create a check at a dead-man's-switch service, for example [healthchecks.io](https://healthchecks.io/): period 5 minutes, grace 10 minutes, notifications to a channel you read. Healthchecks.io accepts `POST` to the check URL for success and to `URL/fail` for failure.
2. Set `heartbeatUrl` in `site.json` to the HTTPS check URL before provisioning. On an existing host, write it to `/etc/nylzo/heartbeat.url` (root, `0600`) and re-apply the host bundle with `sudo /opt/nylzo/runtime/update host vX.Y.Z`.
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
node tools/security/monitor-host.mts
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

- **Released web client and lifecycle**, with `NYLZO_MONITOR_URL`: verifies that the instance serves the signed release's web client and creates, decrypts and consumes a five-minute synthetic message, about 24 creations a day. It prints only pass or fail. It runs the CLI, which a hostile server can recognise ([security model](Security-model.md#the-client-is-part-of-the-trust-boundary)), so it detects careless blanket changes only.
- **Domain**, with `NYLZO_MONITOR_DOMAIN` (and optionally `NYLZO_ACME_ACCOUNT` and `NYLZO_CT_REVIEWED_UNTIL`): the CAA set over a DNSSEC-validating resolver, null MX, SPF and DMARC, the registrar locks and an expiry more than 180 days away, and every certificate issued in the last eight days for the domain or its subdomains in Certificate Transparency, which must come from Let's Encrypt for the apex or `www` only. Set `NYLZO_CT_REVIEWED_UNTIL` to an ISO date to acknowledge certificates you reviewed. Enable it after the DNS stack and registrar lock are applied.
- **Maintenance publication**: a merged maintenance fix still unpublished after an hour.

GitHub schedules are [best effort](https://docs.github.com/en/actions/how-tos/troubleshoot-workflows); rely on the heartbeat for timely alerts. Test notification delivery for each channel; a failed run alone does not prove that a message reached anyone.

References: [Caddy client authentication](https://caddyserver.com/docs/caddyfile/directives/tls#client_auth), [healthchecks.io HTTP API](https://healthchecks.io/docs/http_api/), [Cert Spotter API](https://sslmate.com/help/reference/ct_search_api_v1), [Flatcar update states](https://www.flatcar.org/docs/latest/updates-releases/releases/update-strategies/).
