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

test("Caddy installs only a verified newer build, keeps upstream releases for releases without one, and restores the previous executable on failure", { skip: !available }, () => {
  const releaseSigned = `grep -q '^verify-blob .*--certificate-identity https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/v0.2.1 ' /tmp/verified`;
  const archiveVerified = `grep -qx 'https://github.com/misaon/nylzo/releases/download/v0.2.1/nylzo-caddy_0.2.1_linux_amd64.tar.gz' /tmp/downloads
grep -q '^attestation verify .*nylzo-caddy_0.2.1_linux_amd64.tar.gz .*--source-ref refs/tags/v0.2.1 ' /tmp/verified`;
  const untouched = (previous: string) => `[[ "$status" != 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/${previous}/caddy && ! -s /tmp/restarts ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = ${previous} && ! -e /var/lib/nylzo-deploy/caddy.previous.tag && ! -s /tmp/starts ]]`;
  const installed = (candidate: string, previous: string) => `[[ "$status" = 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/${candidate}/caddy && "$(cat /tmp/restarts)" = /opt/nylzo/caddy/${candidate}/caddy ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = ${candidate} && "$(cat /var/lib/nylzo-deploy/caddy.previous.tag)" = ${previous} ]]`;
  const scenarios: Record<string, { previous: string; listed?: boolean; identical?: boolean; approved?: string; built?: string; current?: string; fail?: string; release?: string; expectation: string }> = {
    "fresh host": { previous: "", expectation: `${releaseSigned}
${archiveVerified}
[[ "$status" = 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/release-v0.2.1/caddy && "$(cat /tmp/restarts)" = /opt/nylzo/caddy/release-v0.2.1/caddy ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = release-v0.2.1 && ! -e /var/lib/nylzo-deploy/caddy.previous.tag ]]` },
    "failed restart": { previous: "release-v0.2.0", fail: "restart", expectation: `${releaseSigned}
${archiveVerified}
[[ "$status" != 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/release-v0.2.0/caddy ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = release-v0.2.0 && "$(wc -l < /tmp/restarts)" = 2 ]]` },
    identical: { previous: "release-v0.2.0", identical: true, expectation: `${releaseSigned}
${archiveVerified}
[[ "$status" = 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/release-v0.2.1/caddy && ! -s /tmp/restarts && -s /tmp/starts ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = release-v0.2.1 && "$(cat /var/lib/nylzo-deploy/caddy.previous.tag)" = release-v0.2.0 ]]` },
    "upstream to release": { previous: "v2.11.7", approved: "v2.11.8", expectation: `${releaseSigned}
${archiveVerified}
if grep -q caddyserver /tmp/downloads; then exit 1; fi
grep -qx -- '--wait --pipe --collect --uid=caddy --gid=caddy /opt/nylzo/caddy/release-v0.2.1/caddy version' /tmp/verified
${installed("release-v0.2.1", "v2.11.7")}` },
    "older release build": { previous: "v2.11.8", built: "v2.11.7", expectation: `${releaseSigned}
${archiveVerified}
${untouched("v2.11.8")}` },
    "another release": { previous: "release-v0.2.0", current: "v0.2.0", expectation: `[[ ! -s /tmp/downloads ]]
${untouched("release-v0.2.0")}` },
    "release without a build": { previous: "release-v0.2.0", listed: false, expectation: `${releaseSigned}
if grep -q 'tar.gz$' /tmp/downloads; then exit 1; fi
[[ "$status" = 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/release-v0.2.0/caddy && ! -s /tmp/restarts && -s /tmp/starts ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = release-v0.2.0 ]]
rm /var/lib/nylzo-deploy/caddy.tag
if PATH="/tmp/bin:$PATH" bash /test-update caddy v0.2.1; then exit 1; fi` },
    "approved upstream release": { previous: "v2.11.7", listed: false, approved: "v2.11.8", expectation: `${releaseSigned}
grep -q '^verify-blob .*upstream-checksums.txt .*--certificate-identity https://github.com/caddyserver/caddy/.github/workflows/release.yml@refs/tags/v2.11.8 ' /tmp/verified
grep -qx 'https://github.com/caddyserver/caddy/releases/download/v2.11.8/caddy_2.11.8_linux_amd64.tar.gz' /tmp/downloads
[[ "$(cat /opt/nylzo/caddy/v2.11.8/caddy)" = 'upstream verified executable' ]]
${installed("v2.11.8", "v2.11.7")}` },
    "installed upstream release": { previous: "v2.11.7", listed: false, expectation: `${releaseSigned}
if grep -q 'tar.gz$' /tmp/downloads; then exit 1; fi
[[ "$status" = 0 && "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/v2.11.7/caddy && ! -s /tmp/restarts && -s /tmp/starts ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = v2.11.7 ]]` },
    "upstream downgrade": { previous: "v2.11.7", listed: false, approved: "v2.11.6", expectation: `${releaseSigned}
if grep -q 'tar.gz$' /tmp/downloads; then exit 1; fi
${untouched("v2.11.7")}` },
    "release downgrade": { previous: "release-v0.2.2", expectation: `[[ ! -s /tmp/downloads ]]
${untouched("release-v0.2.2")}` },
    "mutable release": { previous: "release-v0.2.0", fail: "jq", expectation: `if grep -q 'checksums.txt' /tmp/downloads; then exit 1; fi
${untouched("release-v0.2.0")}` },
    "forged checksums": { previous: "release-v0.2.0", fail: "cosign", expectation: `${releaseSigned}
if grep -q 'tar.gz$' /tmp/downloads; then exit 1; fi
[[ ! -e /opt/nylzo/caddy/release-v0.2.1 ]]
${untouched("release-v0.2.0")}` },
    "forged provenance": { previous: "release-v0.2.0", fail: "gh", expectation: `${releaseSigned}
${archiveVerified}
[[ ! -e /opt/nylzo/caddy/release-v0.2.1 ]]
${untouched("release-v0.2.0")}` },
  };
  for (const [scenario, { previous, listed = true, identical = false, approved, built = "v2.11.8", current = "v0.2.1", fail = "", release = "v0.2.1", expectation }] of Object.entries(scenarios)) {
    const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
      "--tmpfs", "/opt", "--tmpfs", "/var/lib", "--tmpfs", "/var/tmp", "--tmpfs", "/run", "--tmpfs", "/etc/nylzo", "--tmpfs", "/tmp:exec",
      "--mount", `type=bind,source=${path.resolve("deploy/hetzner-flatcar/runtime/update")},target=/test-update,readonly`,
      "ubuntu:24.04@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852", "bash", "-s"], {
      encoding: "utf8", timeout: 60000, input: `set -euo pipefail
mkdir -p /opt/nylzo/bin /var/lib/nylzo-deploy /tmp/bin /tmp/fixture
printf '{"tag":"${current}"}\\n' > /var/lib/nylzo-deploy/current.json
${approved ? `printf '${approved}\\n' > /var/lib/nylzo-deploy/caddy.approved.tag` : ""}
${previous ? `mkdir -p /opt/nylzo/caddy/${previous}
printf '${previous}\\n' > /var/lib/nylzo-deploy/caddy.tag
printf 'previous verified executable\\n' > /opt/nylzo/caddy/${previous}/caddy
ln -s /opt/nylzo/caddy/${previous}/caddy /opt/nylzo/bin/caddy` : ""}
printf '${identical ? "previous" : "candidate"} verified executable\\n' > /tmp/fixture/caddy
(cd /tmp/fixture && tar -czf nylzo-caddy_0.2.1_linux_amd64.tar.gz caddy && sha256sum ${listed ? "nylzo-caddy_0.2.1_linux_amd64.tar.gz" : "caddy"} > checksums.txt)
printf 'upstream verified executable' > /tmp/fixture/caddy
(cd /tmp/fixture && for version in 2.11.6 2.11.8; do tar -czf caddy_\${version}_linux_amd64.tar.gz caddy; done && sha512sum caddy_*_linux_amd64.tar.gz > upstream-checksums.txt)
cat > /tmp/bin/curl <<'CURL'
#!/bin/bash
file="\${@: -1}"
printf '%s\\n' "\${@: -3:1}" >> /tmp/downloads
case "\${file##*/}" in
  checksums.txt | upstream-checksums.txt | *.tar.gz) cp "/tmp/fixture/\${file##*/}" "$file" ;;
  *) printf '{}\\n' > "$file" ;;
esac
CURL
cat > /tmp/bin/jq <<'JQ'
#!/bin/bash
case "$*" in
  *immutable*) [[ "${fail}" != jq ]] ;;
  *'.tag_name == $version'*) [[ -s "\${@: -1}" ]] ;;
  *' .tag '*) sed -n 's/^{"tag":"\\(.*\\)"}$/\\1/p' "\${@: -1}" ;;
  *architecture*) echo amd64 ;;
  *) echo '{}' ;;
esac
JQ
for name in cosign gh; do printf '#!/bin/bash\\nprintf "%%s\\\\n" "$*" >> /tmp/verified\\n[[ "%s" != "${fail}" ]]\\n' "$name" > /tmp/bin/$name; done
cat > /tmp/bin/systemd-run <<'RUN'
#!/bin/bash
printf '%s\\n' "$*" >> /tmp/verified
if [[ "\${@: -1}" = version ]]; then echo '${built} h1:fixture'; fi
RUN
printf '#!/bin/bash\\n' > /tmp/bin/sleep
cat > /tmp/bin/systemctl <<'SYSTEMCTL'
#!/bin/bash
case "$*" in
  'restart caddy.service')
    readlink /opt/nylzo/bin/caddy >> /tmp/restarts
    [[ "${fail}" != restart || "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/${previous}/caddy ]] ;;
  'is-active --quiet caddy.service') ;;
  'start caddy.service') echo started >> /tmp/starts ;;
  *) exit 1 ;;
esac
SYSTEMCTL
chmod 0755 /tmp/bin/*
touch /tmp/restarts /tmp/starts /tmp/downloads /tmp/verified
set +e
PATH="/tmp/bin:$PATH" bash /test-update caddy ${release}
status=$?
set -e
${expectation}
echo VERIFIED_CADDY_UPDATE
`,
    });
    assert.equal(result.status, 0, `${scenario}\n${result.stdout}\n${result.stderr}`);
    assert.match(result.stdout, /VERIFIED_CADDY_UPDATE/u);
  }
});
