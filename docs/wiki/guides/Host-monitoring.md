# Host monitoring

The Hetzner/Flatcar profile can serve a small readiness report on TCP port **8443**. Caddy requires TLS 1.3 and the exact client certificate pinned in the host configuration. This listener serves only `/healthz`; the application remains on port 443.

The report contains five booleans and its observation time. It contains no message data, capabilities, identifiers, peppers, keys, logs, or detailed host inventory. A root-owned systemd service refreshes it atomically every two minutes. Caddy can read the file but cannot write it.

## Create the monitor identity

Use the existing private operator directory created by `deploy init`:

```sh
mise exec go -- go run ./tools/deploy/monitor-identity .deploy/production/monitoring
```

This creates an ECDSA P-256 client identity valid for one year. Existing identities are never overwritten. Keep `client-key.pem` on the operator workstation and in the protected monitoring environment. Only the public `client.pem` belongs on the VPS.

Set `monitorClientCertificate` in the private `site.json` to the PEM contents of `client.pem`. The optional setting enables the readiness units, certificate pin, and firewall rule for TCP 8443. It requires automatic updates. Review the server plan before applying it; the fixed server resources and price remain unchanged.

For a new server, Ignition installs the configuration. For an existing server, apply the reviewed host files and units explicitly as described in [Hetzner/Flatcar](Hetzner-Flatcar.md); changing `user_data` does not update a running host. Install the public certificate before restarting Caddy. Keep application HTTPS restricted during acceptance.

Verify the listener locally without putting secrets in arguments or shell history:

```sh
URL=https://your-domain:8443/healthz \
MONITOR_CERT_FILE=.deploy/production/monitoring/client.pem \
MONITOR_KEY_FILE=.deploy/production/monitoring/client-key.pem \
node tools/security/monitor-host.mts
```

`MONITOR_ADDRESS` can pin the connection to the server's IPv4 or IPv6 address. TLS still verifies the original hostname and trusted server certificate. The pin can be useful during DNS migration; it must be updated if the server address changes.

## Connect GitHub Actions

In the repository hosting `monitor.yml`, create an environment named **host-monitoring**. Restrict its deployment branches to the exact `main` branch. Keep monitoring credentials separate from release and security-automation credentials. The scheduled workflow requires no approval or write token.

Set the client identity as environment secrets through standard input. Replace `YOUR_OWNER/YOUR_REPOSITORY` with the repository hosting the monitor:

```sh
gh secret set NYLZO_MONITOR_CERT --repo YOUR_OWNER/YOUR_REPOSITORY --env host-monitoring \
  < .deploy/production/monitoring/client.pem
gh secret set NYLZO_MONITOR_KEY --repo YOUR_OWNER/YOUR_REPOSITORY --env host-monitoring \
  < .deploy/production/monitoring/client-key.pem
```

Set repository variable `NYLZO_HOST_MONITOR_URL` to `https://your-domain:8443/healthz`. Optionally set `NYLZO_HOST_MONITOR_ADDRESS` to the verified server address. The workflow checks host readiness about every 15 minutes and retries twice across brief restart windows. The separate released-client and synthetic-message checks remain hourly and use about 24 message creations per day.

## Checks and acceptance

- Host policy: SELinux enforcement, effective kernel settings, Docker isolation, disabled CRI/transfer plugins, and the traffic ceiling.
- Services: the application, Caddy, OS readiness, update-engine, and the application update timer are active.
- Disk: at least **8 GiB** is available, before the application's 4 GiB creation cutoff.
- Application updates: no persistent deployment-failure marker and a successful check within 30 minutes, or a check still running within its 15-minute deadline.
- OS updates: update-engine reports a known non-error state and checked within 24 hours. This checks the updater's reported state; it does not prove that every upstream patch is installed.

The client rejects stale observations, future timestamps, missing fields, incorrect types, and unhealthy components. Each check also requires the listener to reject a request without a client certificate; a connection or server-trust error does not count as proof of that denial. It requests rotation when the client identity has 30 days remaining. Reissue into a new private directory, replace the pinned public certificate on the host, and update both environment secrets before expiry.

Before relying on alerts, confirm an authenticated healthy check, refusal without a certificate, refusal with another identity, and a controlled failure followed by recovery in Actions. Test notification delivery using the owner's GitHub Actions notification settings. A failed workflow alone does not prove that an email reached its recipient.

GitHub schedules are [best effort](https://docs.github.com/en/actions/how-tos/troubleshoot-workflows) and may be delayed or disabled after repository inactivity. This is useful external monitoring for a small deployment; use an independent monitor when you need a contractual response time. Keep incident response and [operator recovery](Operations.md) procedures available outside the VPS.

References: [Caddy client authentication](https://caddyserver.com/docs/caddyfile/directives/tls#client_auth), [SNI/Host enforcement](https://caddyserver.com/docs/caddyfile/options#strict_sni_host), and [Flatcar update states](https://www.flatcar.org/docs/latest/updates-releases/releases/update-strategies/).
