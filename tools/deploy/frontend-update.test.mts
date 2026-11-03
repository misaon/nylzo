import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { test } from "node:test";

const available = spawnSync("docker", ["info"], { stdio: "ignore" }).status === 0;
const image = /"(ubuntu:\d+\.\d+@sha256:[a-f0-9]{64})"/u.exec(readFileSync(path.resolve("tools/deploy/runtime.test.mts"), "utf8"))![1]!;

test("frontend activation waits for the image pull, candidate probe and persistent store check", { skip: !available, timeout: 120000 }, () => {
  for (const failure of ["pull", "start", "probe", "store", "activation"]) {
    const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
      "--tmpfs", "/opt:exec", "--tmpfs", "/var/lib", "--tmpfs", "/var/tmp", "--tmpfs", "/run", "--tmpfs", "/etc/nylzo", "--tmpfs", "/tmp:exec",
      "--mount", `type=bind,source=${path.resolve("deploy/hetzner-flatcar/host/update/update")},target=/test-update,readonly`,
      "--env", `FAILURE=${failure}`, image, "bash", "-s"], { encoding: "utf8", timeout: 20000, input: String.raw`set -euo pipefail
trap 'echo "failed at line $LINENO: $BASH_COMMAND" >&2' ERR
mkdir -p /opt/nylzo/runtime /opt/nylzo/bin /opt/nylzo/caddy/release-v0.3.0 /var/lib/nylzo-deploy /tmp/bin
printf '{"tag":"v0.3.0"}\n' > /var/lib/nylzo-deploy/current.json
printf 'release-v0.3.0\n' > /var/lib/nylzo-deploy/caddy.tag
printf 'old verified Caddy frontend\n' > /opt/nylzo/caddy/release-v0.3.0/caddy
ln -s /opt/nylzo/caddy/release-v0.3.0/caddy /opt/nylzo/bin/caddy
printf '#!/bin/bash\necho v0.3.1\n' > /opt/nylzo/runtime/release-target
printf '#!/bin/bash\nexit 0\n' > /opt/nylzo/runtime/refresh-tools
cat > /tmp/bin/jq <<'JQ'
#!/bin/bash
case "$*" in
  '-r .initialRelease '*) echo v0.3.1 ;;
  '-r .tag '*) echo v0.3.0 ;;
  '-er map('*) printf 'sha256:%064d\n' 0 ;;
  *) echo '{}' ;;
esac
JQ
cat > /tmp/bin/curl <<'CURL'
#!/bin/bash
url="${"$"}{@: -3:1}"
file="${"$"}{@: -1}"
if [[ "$url" = */checksums.txt ]]; then
  touch /tmp/activation-attempted
  exit 91
fi
printf '{}\n' > "$file"
CURL
cat > /tmp/bin/docker <<'DOCKER'
#!/bin/bash
printf '%s\n' "$*" >> /tmp/docker
case "$*" in
  'pull '*) [[ "$FAILURE" != pull ]] ;;
  *'--name nylzo-probe '*) [[ "$FAILURE" != start ]] ;;
  'exec nylzo-probe '*) [[ "$FAILURE" != probe ]] ;;
  *'--name nylzo-store-check '*) [[ "$FAILURE" != store ]] ;;
  *) exit 0 ;;
esac
DOCKER
for name in cosign gh chown sleep; do printf '#!/bin/bash\nexit 0\n' > /tmp/bin/$name; done
printf '#!/bin/bash\nprintf "%%064d\\n" 0\n' > /tmp/bin/openssl
printf '#!/bin/bash\necho "$*" >> /tmp/systemctl\n' > /tmp/bin/systemctl
chmod 0755 /tmp/bin/* /opt/nylzo/runtime/*
touch /tmp/docker /tmp/systemctl
set +e
PATH="/tmp/bin:$PATH" bash /test-update nylzo
status=$?
set -e
[[ "$status" != 0 ]]
[[ "$(cat /var/lib/nylzo-deploy/current.json)" = '{"tag":"v0.3.0"}' ]]
[[ "$(cat /var/lib/nylzo-deploy/caddy.tag)" = release-v0.3.0 ]]
[[ "$(readlink /opt/nylzo/bin/caddy)" = /opt/nylzo/caddy/release-v0.3.0/caddy ]]
[[ ! -e /var/lib/nylzo-deploy/failed.json && ! -s /tmp/systemctl ]]
case "$FAILURE" in
  activation)
    [[ -e /tmp/activation-attempted ]]
    grep -q -- '--name nylzo-store-check ' /tmp/docker
    ;;
  probe)
    [[ ! -e /tmp/activation-attempted ]]
    [[ "$(grep -c '^exec nylzo-probe ' /tmp/docker)" = 30 ]]
    ;;
  store)
    [[ ! -e /tmp/activation-attempted ]]
    grep -q -- '--name nylzo-store-check ' /tmp/docker
    ;;
  *) [[ ! -e /tmp/activation-attempted ]] ;;
esac
echo FRONTEND_PRESERVED
` });
    assert.equal(result.status, 0, `${failure}\n${result.stdout}\n${result.stderr}`);
    assert.match(result.stdout, /FRONTEND_PRESERVED/u);
  }
});
