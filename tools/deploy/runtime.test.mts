import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { test } from "node:test";

const available = spawnSync("docker", ["info"], { stdio: "ignore" }).status === 0;
test("a failed OS health-marker flush preserves the previous recovery target", { skip: !available }, () => {
  for (const fail of [false, true]) {
    const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
      "--tmpfs", "/opt:exec", "--tmpfs", "/var/lib",
      "--mount", `type=bind,source=${path.resolve("deploy/hetzner-flatcar/runtime/health")},target=/test-health,readonly`,
      "ubuntu:24.04@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852", "bash", "-s"], {
      encoding: "utf8", timeout: 60000, input: `set -euo pipefail
umask 077
mkdir -p /opt/nylzo/bin /opt/nylzo/runtime /var/lib/nylzo-deploy
printf '4000.1.0\\n' > /var/lib/nylzo-deploy/last-good-os
for name in systemctl; do printf '#!/bin/bash\\nexit 0\\n' > /opt/nylzo/bin/$name; done
printf '#!/bin/bash\\nexit 0\\n' > /opt/nylzo/runtime/host-policy
printf '#!/bin/bash\\necho "{\\"build\\":{\\"version\\":\\"0.1.5\\",\\"dirty\\":false}}"\\n' > /opt/nylzo/bin/curl
cat > /opt/nylzo/bin/jq <<'JQ'
#!/bin/bash
if [[ "$*" = *current.json* ]]; then echo v0.1.5; else cat >/dev/null; fi
JQ
${fail ? "printf '#!/bin/bash\\nexit 1\\n' > /opt/nylzo/bin/sync" : ""}
chmod 0755 /opt/nylzo/bin/* /opt/nylzo/runtime/host-policy
set +e
bash /test-health
status=$?
set -e
${fail ? '[[ "$status" = 1 && "$(cat /var/lib/nylzo-deploy/last-good-os)" = 4000.1.0 ]]' : '[[ "$status" = 0 && "$(cat /var/lib/nylzo-deploy/last-good-os)" = "$(sed -n \'s/^VERSION_ID=//p\' /etc/os-release | tr -d \'"\')" ]]'}
[[ "$(stat -c %a /var/lib/nylzo-deploy/last-good-os)" = 600 ]]
[[ -z "$(find /var/lib/nylzo-deploy -name '.last-good-os.*' -print -quit)" ]]
echo OS_RECOVERY_MARKER_VERIFIED
`,
    });
    assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
    assert.match(result.stdout, /OS_RECOVERY_MARKER_VERIFIED/u);
  }
});

test("host-tool tampering fails closed and preserves a previously verified generation", { skip: !available }, () => {
  for (const previous of [false, true]) {
    const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
      "--tmpfs", "/opt", "--tmpfs", "/var/cache", "--tmpfs", "/etc/nylzo", "--tmpfs", "/tmp:exec",
      "--mount", `type=bind,source=${path.resolve("deploy/hetzner-flatcar/runtime/install-tools")},target=/test-install,readonly`,
      "ubuntu:24.04@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852", "bash", "-s"], {
      encoding: "utf8", timeout: 60000,
      input: `set -euo pipefail
mkdir -p /tmp/bin /opt/nylzo/tools
cat > /tmp/bin/curl <<'CURL'
#!/bin/bash
printf '#!/bin/bash\\ntouch /tmp/unverified-executed\\n' > "\${@: -1}"
CURL
chmod 0755 /tmp/bin/curl
cat > /etc/nylzo/tools.env <<'ENV'
ARCHITECTURE=amd64
JQ_VERSION=1.8.2
JQ_SHA256=${"0".repeat(64)}
COSIGN_VERSION=3.1.4
COSIGN_SHA256=${"0".repeat(64)}
GH_VERSION=2.103.3
GH_SHA256=${"0".repeat(64)}
ENV
${previous ? `mkdir -p /opt/nylzo/tools/previous
for name in gh jq cosign; do printf 'previous verified generation\\n' > /opt/nylzo/tools/previous/$name; done
sha256sum /opt/nylzo/tools/previous/{gh,jq,cosign} > /opt/nylzo/tools/previous/checksums.txt
ln -s /opt/nylzo/tools/previous /opt/nylzo/tools/current` : ""}
set +e
PATH="/tmp/bin:$PATH" bash /test-install
status=$?
set -e
[[ ! -e /tmp/unverified-executed && ! -e /opt/nylzo/bin/jq ]]
${previous ? '[[ "$status" = 0 && "$(readlink /opt/nylzo/tools/current)" = /opt/nylzo/tools/previous ]]' : '[[ "$status" != 0 && ! -e /opt/nylzo/tools/current ]]'}
echo VERIFIED_FAILURE_BEHAVIOR
`,
    });
    assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
    assert.match(result.stdout, /VERIFIED_FAILURE_BEHAVIOR/u);
  }
});

test("a Caddy candidate from the verified release restarts only when it changed and restores the previous executable on failure", { skip: !available }, () => {
  const verified = `grep -qx 'https://github.com/misaon/nylzo/releases/download/v0.2.1/nylzo-caddy_0.2.1_linux_amd64.tar.gz' /tmp/downloads
grep -q '^attestation verify .*nylzo-caddy_0.2.1_linux_amd64.tar.gz .*--source-ref refs/tags/v0.2.1 ' /tmp/verified`;
  const expectations = {
    changed: `${verified}
[[ "$status" != 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/v0.2.0/caddy ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = v0.2.0 && "$(wc -l < /tmp/restarts)" = 2 ]]`,
    identical: `${verified}
[[ "$status" = 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/v0.2.1/caddy && ! -s /tmp/restarts ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = v0.2.1 && "$(cat /var/lib/nylzo-deploy/caddy.previous.tag)" = v0.2.0 ]]`,
    absent: `if grep -q 'tar.gz$' /tmp/downloads; then exit 1; fi
[[ "$status" = 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/v0.2.0/caddy && ! -s /tmp/restarts ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = v0.2.0 ]]
rm /var/lib/nylzo-deploy/caddy.tag
if PATH="/tmp/bin:$PATH" bash /test-update caddy v0.2.1; then exit 1; fi`,
  };
  for (const [scenario, expectation] of Object.entries(expectations)) {
    const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
      "--tmpfs", "/opt", "--tmpfs", "/var/lib", "--tmpfs", "/var/tmp", "--tmpfs", "/run", "--tmpfs", "/etc/nylzo", "--tmpfs", "/tmp:exec",
      "--mount", `type=bind,source=${path.resolve("deploy/hetzner-flatcar/runtime/update")},target=/test-update,readonly`,
      "ubuntu:24.04@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852", "bash", "-s"], {
      encoding: "utf8", timeout: 60000, input: `set -euo pipefail
mkdir -p /opt/nylzo/bin /opt/nylzo/caddy/v0.2.0 /var/lib/nylzo-deploy /tmp/bin /tmp/fixture
printf 'v0.2.0\\n' > /var/lib/nylzo-deploy/caddy.tag
printf 'previous verified executable\\n' > /opt/nylzo/caddy/v0.2.0/caddy
ln -s /opt/nylzo/caddy/v0.2.0/caddy /opt/nylzo/bin/caddy
printf '${scenario === "identical" ? "previous" : "candidate"} verified executable\\n' > /tmp/fixture/caddy
(cd /tmp/fixture && tar -czf nylzo-caddy_0.2.1_linux_amd64.tar.gz caddy && sha256sum ${scenario === "absent" ? "caddy" : "nylzo-caddy_0.2.1_linux_amd64.tar.gz"} > checksums.txt)
cat > /tmp/bin/curl <<'CURL'
#!/bin/bash
file="\${@: -1}"
printf '%s\\n' "\${@: -3:1}" >> /tmp/downloads
case "\${file##*/}" in
  checksums.txt) cp /tmp/fixture/checksums.txt "$file" ;;
  *.tar.gz) cp /tmp/fixture/nylzo-caddy_0.2.1_linux_amd64.tar.gz "$file" ;;
  *) printf '{}\\n' > "$file" ;;
esac
CURL
cat > /tmp/bin/jq <<'JQ'
#!/bin/bash
case "$*" in
  *architecture*) echo amd64 ;;
  *) echo '{}' ;;
esac
JQ
for name in cosign gh systemd-run; do printf '#!/bin/bash\\nprintf "%%s\\\\n" "$*" >> /tmp/verified\\nexit 0\\n' > /tmp/bin/$name; done
cat > /tmp/bin/systemctl <<'SYSTEMCTL'
#!/bin/bash
if [[ "$*" = 'restart caddy.service' ]]; then
  readlink /opt/nylzo/bin/caddy >> /tmp/restarts
  [[ "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/v0.2.0/caddy ]]
else exit 1
fi
SYSTEMCTL
chmod 0755 /tmp/bin/*
touch /tmp/restarts
set +e
PATH="/tmp/bin:$PATH" bash /test-update caddy v0.2.1
status=$?
set -e
grep -q '^verify-blob .*--certificate-identity https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/v0.2.1 ' /tmp/verified
${expectation}
echo VERIFIED_CADDY_UPDATE
`,
    });
    assert.equal(result.status, 0, `${scenario}\n${result.stdout}\n${result.stderr}`);
    assert.match(result.stdout, /VERIFIED_CADDY_UPDATE/u);
  }
});
