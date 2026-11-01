import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { test } from "node:test";

const available = spawnSync("docker", ["info"], { stdio: "ignore" }).status === 0;
const image = /"(ubuntu:\d+\.\d+@sha256:[a-f0-9]{64})"/u.exec(readFileSync(path.resolve("tools/deploy/runtime.test.mts"), "utf8"))![1]!;

test("hosts install only a host bundle listed in signed checksums with release provenance, and restore the previous one on failure", { skip: !available, timeout: 300000 }, () => {
  const scenarios: Record<string, { listed?: boolean; fail?: string; escape?: boolean; expectation: string }> = {
    installed: { expectation: String.raw`[[ "$status" = 0 && "$(cat /tmp/activated)" = /opt/nylzo/host/release-v0.3.1 ]]
grep -q '^verify-blob .*--certificate-identity https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/v0.3.1 ' /tmp/verified
grep -q '^attestation verify .*nylzo-host_0.3.1.tar .*--source-ref refs/tags/v0.3.1 ' /tmp/verified` },
    "release without a bundle": { listed: false, expectation: String.raw`[[ "$status" = 0 && ! -e /tmp/activated && ! -e /opt/nylzo/host/release-v0.3.1 ]]
if grep -q 'nylzo-host_0.3.1.tar$' /tmp/downloads; then exit 1; fi` },
    "forged checksums": { fail: "cosign", expectation: String.raw`[[ "$status" != 0 && ! -e /tmp/activated && ! -e /opt/nylzo/host/release-v0.3.1 ]]` },
    "forged provenance": { fail: "gh", expectation: String.raw`[[ "$status" != 0 && ! -e /tmp/activated && ! -e /opt/nylzo/host/release-v0.3.1 ]]` },
    "unexpected path": { escape: true, expectation: String.raw`[[ "$status" != 0 && ! -e /tmp/activated && ! -e /opt/nylzo/escape ]]` },
    "failed checks": { fail: "host-policy", expectation: String.raw`[[ "$status" != 0 && "$(cat /tmp/activated)" = "$(printf '%s\n' /opt/nylzo/host/release-v0.3.1 /opt/nylzo/host/initial)" ]]` },
  };
  for (const [scenario, { listed = true, fail = "", escape = false, expectation }] of Object.entries(scenarios)) {
    const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
      "--tmpfs", "/opt:exec", "--tmpfs", "/var/lib", "--tmpfs", "/var/tmp", "--tmpfs", "/run", "--tmpfs", "/etc/nylzo", "--tmpfs", "/tmp:exec",
      "--mount", `type=bind,source=${path.resolve("deploy/hetzner-flatcar/host/update/update")},target=/test-update,readonly`,
      image, "bash", "-s"], { encoding: "utf8", timeout: 60000, input: String.raw`set -euo pipefail
trap 'echo "failed at line $LINENO: $BASH_COMMAND" >&2' ERR
mkdir -p /opt/nylzo/runtime /opt/nylzo/host/initial/update /var/lib/nylzo-deploy /tmp/bin /tmp/fixture/bundle/update
printf '{"tag":"v0.3.1"}\n' > /var/lib/nylzo-deploy/current.json
printf '#!/bin/bash\nprintf "%%s\\n" "$1" >> /tmp/activated\n' > /opt/nylzo/host/initial/update/activate
cp /opt/nylzo/host/initial/update/activate /tmp/fixture/bundle/update/activate
printf 'format 1\n' > /tmp/fixture/bundle/manifest
ln -s /opt/nylzo/host/initial /opt/nylzo/host/current
printf '#!/bin/bash\n[[ "${fail}" != host-policy ]]\n' > /opt/nylzo/runtime/host-policy
(cd /tmp/fixture/bundle && tar --create --file /tmp/fixture/nylzo-host_0.3.1.tar manifest update/ update/activate ${escape ? "--transform 's,^manifest$,../escape,'" : ""})
(cd /tmp/fixture && sha256sum ${listed ? "nylzo-host_0.3.1.tar" : "bundle/manifest"} > checksums.txt)
cat > /tmp/bin/curl <<'CURL'
#!/bin/bash
file="${"$"}{@: -1}"
printf '%s\n' "${"$"}{@: -3:1}" >> /tmp/downloads
case "${"$"}{file##*/}" in
  checksums.txt | nylzo-host_0.3.1.tar) cp "/tmp/fixture/${"$"}{file##*/}" "$file" ;;
  *) printf '{}\n' > "$file" ;;
esac
CURL
cat > /tmp/bin/jq <<'JQ'
#!/bin/bash
case "$*" in
  *immutable*) ;;
  *'.tag_name == $version'*) [[ -s "${"$"}{@: -1}" ]] ;;
  *' .tag '*) sed -n 's/^{"tag":"\(.*\)"}$/\1/p' "${"$"}{@: -1}" ;;
  *) echo '{}' ;;
esac
JQ
for name in cosign gh; do printf '#!/bin/bash\nprintf "%%s\\n" "$*" >> /tmp/verified\n[[ "%s" != "${fail}" ]]\n' "$name" > /tmp/bin/$name; done
printf '#!/bin/bash\n' > /tmp/bin/sleep
printf '#!/bin/bash\nexit 3\n' > /tmp/bin/systemctl
chmod 0755 /tmp/bin/* /opt/nylzo/runtime/host-policy
touch /tmp/downloads /tmp/verified
set +e
PATH="/tmp/bin:$PATH" bash /test-update host v0.3.1
status=$?
set -e
${expectation}
if PATH="/tmp/bin:$PATH" bash /test-update host v0.3.0 2>/dev/null; then exit 1; fi
echo VERIFIED_HOST_BUNDLE
` });
    assert.equal(result.status, 0, `${scenario}\n${result.stdout}\n${result.stderr}`);
    assert.match(result.stdout, /VERIFIED_HOST_BUNDLE/u);
  }
});
