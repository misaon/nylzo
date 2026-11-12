import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";
import { test } from "node:test";

const available = spawnSync("docker", ["info"], { stdio: "ignore" }).status === 0;
const image = /"(ubuntu:\d+\.\d+@sha256:[a-f0-9]{64})"/u.exec(readFileSync(path.resolve("tools/deploy/runtime.test.mts"), "utf8"))![1]!;
const { server } = JSON.parse(readFileSync(path.resolve("spec/vectors.json"), "utf8")) as {
  server: { pepper_hex: string; pepper_id: number; previous_pepper_hex: string; previous_pepper_id: number };
};
const pepperId = (hex: string) => createHash("sha256").update("nylzo/v1/pepper-id").update(Buffer.from(hex, "hex")).digest()[0]!;

test("a pepper rotation keeps the previous pepper for serve and never repeats its pepper_id", { skip: !available, timeout: 120000 }, () => {
  assert.equal(pepperId(server.pepper_hex), server.pepper_id);
  assert.equal(pepperId(server.previous_pepper_hex), server.previous_pepper_id);
  let counter = 0;
  let colliding: string;
  do colliding = createHash("sha256").update(`collision/${counter++}`).digest("hex");
  while (pepperId(colliding) !== server.previous_pepper_id);
  const script = (name: string) => `type=bind,source=${path.resolve(`deploy/hetzner-flatcar/host/application/${name}`)},target=/test/${name},readonly`;
  const result = spawnSync("docker", ["run", "--interactive", "--rm", "--network", "none", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--cap-add", "DAC_OVERRIDE", "--cap-add", "FOWNER",
    "--mount", script("prepare"), "--mount", script("run"), image, "bash", "-s"], { encoding: "utf8", timeout: 100000, input: String.raw`set -euo pipefail
trap 'echo "failed at line $LINENO: $BASH_COMMAND" >&2' ERR
mkdir -p /opt/nylzo/runtime /opt/nylzo/bin /usr/lib/systemd /tmp/bin
fake() { printf '#!/bin/bash\n%s\n' "$2" > "$1"; chmod 0755 "$1"; }
fake /usr/lib/systemd/systemd-sysctl 'exit 0'
fake /opt/nylzo/runtime/host-policy 'exit 0'
fake /opt/nylzo/bin/jq 'case "$*" in *.digest*) echo sha256:${"0".repeat(64)} ;; *userns*) echo true ;; esac'
fake /tmp/bin/iptables 'exit 0'
fake /tmp/bin/ip6tables 'exit 0'
fake /tmp/bin/docker '[[ "$1" != run ]] || printf "%s\n" "$@" > /tmp/docker-run'
fake /tmp/bin/openssl 'head -n 1 /tmp/candidates; sed -i 1d /tmp/candidates'
export PATH="/tmp/bin:$PATH"
secrets=/var/lib/nylzo-secrets
owned() { [[ "$(stat -c '%u:%g %a' "$1")" = '165532:165532 400' ]]; }

printf '%s\n' ${server.pepper_hex} > /tmp/candidates
bash /test/prepare
[[ "$(cat "$secrets/pepper")" = ${server.pepper_hex} && ! -e "$secrets/pepper.previous" ]]
owned "$secrets/pepper"
bash /test/run
grep -qx /var/lib/nylzo-secrets/pepper:/run/nylzo-pepper:ro,Z /tmp/docker-run
if grep -q previous /tmp/docker-run; then exit 1; fi

rm "$secrets/pepper"
printf '%s\n' ${server.previous_pepper_hex} > "$secrets/pepper.previous"
chmod 0644 "$secrets/pepper.previous"
printf '%s\n' ${colliding} ${server.pepper_hex} > /tmp/candidates
bash /test/prepare
[[ "$(cat "$secrets/pepper")" = ${server.pepper_hex} && ! -s /tmp/candidates ]]
owned "$secrets/pepper"
owned "$secrets/pepper.previous"
bash /test/run
grep -qx NYLZO_PEPPER_PREVIOUS_FILE=/run/nylzo-pepper-previous /tmp/docker-run
grep -qx /var/lib/nylzo-secrets/pepper.previous:/run/nylzo-pepper-previous:ro,Z /tmp/docker-run

rm "$secrets/pepper" /tmp/docker-run
if bash /test/run 2>/dev/null; then exit 1; fi
[[ ! -e /tmp/docker-run && ! -e "$secrets/pepper" ]]
` });
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
});
