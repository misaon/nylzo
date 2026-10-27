import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { test } from "node:test";

const available = spawnSync("docker", ["info"], { stdio: "ignore" }).status === 0;
test("host-tool tampering fails closed and preserves a previously verified generation", { skip: !available }, () => {
  for (const previous of [false, true]) {
    const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
      "--tmpfs", "/opt", "--tmpfs", "/var/cache", "--tmpfs", "/etc/nylzo", "--tmpfs", "/tmp:exec",
      "--mount", `type=bind,source=${path.resolve("deploy/hetzner-flatcar/runtime/install-tools")},target=/test-install,readonly`,
      "ubuntu@sha256:a853f94d226358a79c740cfc7bce0c289748f3fe3488d921d038ccd752c61b60", "bash", "-s"], {
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

test("a Caddy candidate that cannot restart restores the previously verified executable", { skip: !available }, () => {
  const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
    "--tmpfs", "/opt", "--tmpfs", "/var/lib", "--tmpfs", "/var/tmp", "--tmpfs", "/run", "--tmpfs", "/etc/nylzo", "--tmpfs", "/tmp:exec",
    "--mount", `type=bind,source=${path.resolve("deploy/hetzner-flatcar/runtime/update")},target=/test-update,readonly`,
    "ubuntu@sha256:a853f94d226358a79c740cfc7bce0c289748f3fe3488d921d038ccd752c61b60", "bash", "-s"], {
    encoding: "utf8", timeout: 60000, input: `set -euo pipefail
mkdir -p /opt/nylzo/bin /opt/nylzo/caddy/v2.11.6 /var/lib/nylzo-deploy /tmp/bin /tmp/fixture
printf 'v2.11.6\\n' > /var/lib/nylzo-deploy/caddy.tag
printf 'previous verified executable\\n' > /opt/nylzo/caddy/v2.11.6/caddy
ln -s /opt/nylzo/caddy/v2.11.6/caddy /opt/nylzo/bin/caddy
printf 'verified candidate executable\\n' > /tmp/fixture/caddy
(cd /tmp/fixture && tar -czf caddy_2.11.7_linux_amd64.tar.gz caddy && sha512sum caddy_2.11.7_linux_amd64.tar.gz > checksums.txt)
cat > /tmp/bin/curl <<'CURL'
#!/bin/bash
file="\${@: -1}"
case "\${file##*/}" in
  checksums.txt) cp /tmp/fixture/checksums.txt "$file" ;;
  *.tar.gz) cp /tmp/fixture/caddy_2.11.7_linux_amd64.tar.gz "$file" ;;
  *) printf '{}\\n' > "$file" ;;
esac
CURL
cat > /tmp/bin/jq <<'JQ'
#!/bin/bash
case "$*" in
  *architecture*) echo amd64 ;;
  *tag_name*) echo v2.11.7 ;;
  *) echo 2.11.7 ;;
esac
JQ
for name in cosign systemd-run; do printf '#!/bin/bash\\nexit 0\\n' > /tmp/bin/$name; done
cat > /tmp/bin/systemctl <<'SYSTEMCTL'
#!/bin/bash
if [[ "$*" = 'restart caddy.service' ]]; then
  readlink /opt/nylzo/bin/caddy >> /tmp/restarts
  [[ "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/v2.11.6/caddy ]]
else exit 1
fi
SYSTEMCTL
chmod 0755 /tmp/bin/*
set +e
PATH="/tmp/bin:$PATH" bash /test-update caddy
status=$?
set -e
[[ "$status" != 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/v2.11.6/caddy ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = v2.11.6 && "$(wc -l < /tmp/restarts)" = 2 ]]
echo VERIFIED_CADDY_RECOVERY
`,
  });
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  assert.match(result.stdout, /VERIFIED_CADDY_RECOVERY/u);
});
