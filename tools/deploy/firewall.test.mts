import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

const available = spawnSync("docker", ["info"], { stdio: "ignore" }).status === 0;
const image = /"(ubuntu:\d+\.\d+@sha256:[a-f0-9]{64})"/u.exec(readFileSync(resolve("tools/deploy/runtime.test.mts"), "utf8"))![1]!;

test("the host firewall admits only what the cloud firewall admits and limits each source to 64 web connections", { skip: !available, timeout: 300000 }, () => {
  const result = spawnSync("docker", ["run", "--interactive", "--rm", "--cap-add", "NET_ADMIN",
    "--mount", `type=bind,source=${resolve("deploy/hetzner-flatcar/host/system/firewall")},target=/firewall,readonly`, image, "bash", "-s"], {
    encoding: "utf8", timeout: 280000, input: String.raw`set -euo pipefail
trap 'echo "failed at line $LINENO: $BASH_COMMAND" >&2' ERR
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null && apt-get install -qq -y --no-install-recommends nftables >/dev/null
mkdir -p /etc/nylzo
run() { printf '%s\n' "$@" > /tmp/host.env; NYLZO_HOST_SETTINGS=/tmp/host.env bash /firewall; }
has() { nft --numeric-protocol list chain inet nylzo input > /tmp/rules; grep -Fq -- "$1" /tmp/rules; }
run PUBLIC_ACCESS=false ADMINISTRATOR_NETWORKS=192.0.2.1/32,2001:db8::/64
has 'type filter hook input priority filter; policy drop;'
has 'ip saddr @administrators4 tcp dport 22 accept'
has 'ip6 saddr @administrators6 tcp dport 22 accept'
has 'ip saddr @administrators4 meta l4proto { 6, 17 } th dport 443 accept'
has 'ip saddr ct count over 64'
has 'ip6 saddr & ffff:ffff:ffff:ffff:: ct count over 64'
if has 'dport 8443'; then exit 1; fi
if has 'meta nfproto ipv4 meta l4proto { 6, 17 } th dport 443 accept'; then exit 1; fi
nft list set inet nylzo administrators4 > /tmp/set
grep -Fq '192.0.2.1' /tmp/set
printf 'certificate\n' > /etc/nylzo/monitor-client.pem
run PUBLIC_ACCESS=true ADMINISTRATOR_NETWORKS=192.0.2.0/24
has 'tcp dport 8443 accept'
has 'meta nfproto ipv4 meta l4proto { 6, 17 } th dport 443 accept'
nft list tables > /tmp/tables
[[ "$(grep -c 'inet nylzo' /tmp/tables)" = 1 ]]
for invalid in 'ADMINISTRATOR_NETWORKS=0.0.0.0/0' 'ADMINISTRATOR_NETWORKS=192.0.2.1/16' 'ADMINISTRATOR_NETWORKS=::/0' 'ADMINISTRATOR_NETWORKS=' 'ADMINISTRATOR_NETWORKS=192.0.2.1/32;flush ruleset'; do
  if run PUBLIC_ACCESS=false "$invalid" 2>/dev/null; then echo "accepted $invalid" >&2; exit 1; fi
done
if run PUBLIC_ACCESS=yes ADMINISTRATOR_NETWORKS=192.0.2.1/32 2>/dev/null; then exit 1; fi
has 'tcp dport 8443 accept'
echo FIREWALL_VERIFIED
` });
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  assert.match(result.stdout, /FIREWALL_VERIFIED/u);
});

test("the heartbeat reports only up or down, and down whenever the host status is stale or failing", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-heartbeat-"));
  try {
    writeFileSync(join(directory, "curl"), `#!/bin/sh\nprintf '%s\\n' "$@" > "${directory}/request"\n`);
    chmodSync(join(directory, "curl"), 0o700);
    const run = (status: unknown, address = "https://hc-ping.com/0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d") => {
      writeFileSync(join(directory, "url"), `${address}\n`);
      writeFileSync(join(directory, "status.json"), typeof status === "string" ? status : JSON.stringify(status));
      rmSync(join(directory, "request"), { force: true });
      const result = spawnSync("bash", [resolve("deploy/hetzner-flatcar/host/monitoring/heartbeat")], { encoding: "utf8", timeout: 10000,
        env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, NYLZO_HEARTBEAT_URL_FILE: join(directory, "url"), NYLZO_HOST_STATUS: join(directory, "status.json") } });
      return { status: result.status, request: result.status === 0 ? readFileSync(join(directory, "request"), "utf8").trim().split("\n") : [] };
    };
    const now = Math.floor(Date.now() / 1000);
    const healthy = { version: 1, observedAt: now, checks: { hostPolicy: true, services: true, clock: true } };
    const up = run(healthy);
    assert.equal(up.status, 0);
    assert.equal(up.request.at(-1), "https://hc-ping.com/0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d");
    assert(up.request.includes("--proto") && up.request.includes("=https") && up.request.includes("--data-binary") && up.request.at(up.request.indexOf("--data-binary") + 1) === "");
    for (const down of [{ ...healthy, checks: { ...healthy.checks, clock: false } }, { ...healthy, observedAt: now - 400 }, { ...healthy, observedAt: now + 60 }, "", "not json"]) {
      assert.equal(run(down).request.at(-1), "https://hc-ping.com/0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d/fail", JSON.stringify(down));
    }
    for (const address of ["http://hc-ping.com/x", "https://hc-ping.com/x?token=1", "https://hc-ping.com/x#fragment", "https://user@hc-ping.com/x"]) assert.notEqual(run(healthy, address).status, 0, address);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
