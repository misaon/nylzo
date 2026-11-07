import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

test("the host applies only a newer, correctly signed production directive", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-directive-"));
  try {
    const bin = join(directory, "bin"), published = join(directory, "published"), state = join(directory, "state");
    mkdirSync(bin); mkdirSync(published); mkdirSync(state);
    writeFileSync(join(bin, "curl"), `#!/bin/bash
url="" output=""
while (( $# )); do case "$1" in --output) output="$2"; shift 2 ;; https://*) url="$1"; shift ;; *) shift ;; esac; done
[[ -f "${published}/\${url##*/}" ]] || exit 22
cp "${published}/\${url##*/}" "$output"
`);
    writeFileSync(join(bin, "cosign"), `#!/bin/bash
[[ "$*" = *"--certificate-identity https://github.com/misaon/nylzo/.github/workflows/watch.yml@refs/heads/main "* ]] || exit 1
[[ "$(cat "$4")" = signed-for-$(sha256sum "$2" | cut -d ' ' -f 1) ]]
`);
    chmodSync(join(bin, "curl"), 0o700); chmodSync(join(bin, "cosign"), 0o700);
    const publish = (directive: object, signed = true) => {
      const text = JSON.stringify(directive);
      writeFileSync(join(published, "directive.json"), text);
      const digest = spawnSync("sha256sum", { input: text, encoding: "utf8" }).stdout.split(" ")[0];
      writeFileSync(join(published, "directive.json.sigstore.json"), signed ? `signed-for-${digest}` : "forged");
    };
    const run = () => spawnSync("bash", [resolve("deploy/hetzner-flatcar/host/monitoring/directive")], {
      encoding: "utf8", timeout: 30000,
      env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, NYLZO_DIRECTIVE_SOURCE: "https://raw.example/production-directive", NYLZO_DIRECTIVE_DIR: state },
    });
    const maintenance = () => existsSync(join(state, "maintenance"));

    assert.equal(run().status, 0, "no directive published yet");
    assert.equal(maintenance(), false);
    publish({ format: 1, state: "maintenance", issued: 100, run: "1" });
    assert.equal(run().status, 0);
    assert.equal(maintenance(), true);
    assert.equal(readFileSync(join(state, "issued"), "utf8"), "100\n");
    publish({ format: 1, state: "normal", issued: 50, run: "0" });
    assert.equal(run().status, 0);
    assert.equal(maintenance(), true, "an older directive cannot end maintenance");
    publish({ format: 1, state: "normal", issued: 200, run: "2" }, false);
    assert.notEqual(run().status, 0, "a forged directive fails");
    assert.equal(maintenance(), true);
    publish({ format: 1, state: "lockdown", issued: 300, run: "3" });
    assert.notEqual(run().status, 0, "an unknown state fails");
    assert.equal(maintenance(), true);
    publish({ format: 1, state: "normal", issued: 200, run: "2" });
    assert.equal(run().status, 0);
    assert.equal(maintenance(), false);
    assert.equal(readFileSync(join(state, "issued"), "utf8"), "200\n");
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
