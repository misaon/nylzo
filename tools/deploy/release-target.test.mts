import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

const available = ["jq", "sha256sum"].every(command => spawnSync("bash", ["-c", `command -v ${command}`], { stdio: "ignore" }).status === 0);

type Release = { tag: string; kind?: string; draft?: boolean; prerelease?: boolean };

test("a host that missed a planned release reaches the newest approved line and still gets patches of its own line", { skip: !available }, () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-release-target-"));
  try {
    const bin = join(root, "bin");
    mkdirSync(bin);
    const gnu = spawnSync("bash", ["-c", "command -v gsha256sum"], { encoding: "utf8" });
    if (gnu.status === 0) writeFileSync(join(bin, "sha256sum"), `#!/bin/sh\nexec "${gnu.stdout.trim()}" "$@"\n`);
    writeFileSync(join(bin, "curl"), '#!/usr/bin/env bash\nset -eu\nurl="${@: -3:1}"\nname=$(printf "%s" "${url#https://}" | tr "/?=" "___")\n[[ -e "$FIXTURE/$name" ]] || exit 22\ncp "$FIXTURE/$name" "${@: -1}"\n');
    writeFileSync(join(bin, "cosign"), '#!/usr/bin/env bash\nexit "${SIGNATURE_STATUS:-0}"\n');
    for (const name of ["curl", "cosign", ...(gnu.status === 0 ? ["sha256sum"] : [])]) chmodSync(join(bin, name), 0o700);
    symlinkSync(resolve("deploy/hetzner-flatcar/host/update/release-policy"), join(bin, "release-policy"));
    const run = (current: string, latest: Release, releases: Release[], environment: Record<string, string> = {}) => {
      const fixture = mkdtempSync(join(root, "fixture-"));
      const document = (release: Release) => ({ tag_name: release.tag, draft: release.draft ?? false, prerelease: release.prerelease ?? false });
      const file = (url: string, contents: string) => writeFileSync(join(fixture, url.replace(/[/?=]/gu, "_")), contents);
      file("api.github.com/repos/misaon/nylzo/releases/latest", JSON.stringify(document(latest)));
      file("api.github.com/repos/misaon/nylzo/releases?per_page=100", JSON.stringify(releases.map(document)));
      for (const release of [latest, ...releases]) {
        if (!release.kind) continue;
        const version = release.tag.slice(1);
        const policy = JSON.stringify({ format: 1, tag: release.tag, source: "a".repeat(40), kind: release.kind, policyCommit: "b".repeat(40) });
        const base = `github.com/misaon/nylzo/releases/download/${release.tag}`;
        file(`${base}/nylzo_${version}.release-policy.json`, policy);
        file(`${base}/nylzo_${version}.evidence.sha256`, `${createHash("sha256").update(policy).digest("hex")}  ./nylzo_${version}.release-policy.json\n`);
        file(`${base}/nylzo_${version}.evidence.sha256.sigstore.json`, "{}");
      }
      return spawnSync("bash", [resolve("deploy/hetzner-flatcar/host/update/release-target"), current], { encoding: "utf8", timeout: 20000,
        env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, FIXTURE: fixture, ...environment } });
    };
    const expect = (result: ReturnType<typeof run>, target: string | undefined) => {
      if (target === undefined) assert.notEqual(result.status, 0, result.stdout);
      else assert.equal(result.status, 0, result.stderr);
      assert.equal(result.stdout.trim(), target ?? "");
    };
    const security = { tag: "v0.2.3", kind: "security" };
    const planned = { tag: "v0.2.0", kind: "planned" };
    expect(run("v0.1.12", security, [security, planned]), "v0.2.3");
    expect(run("v0.1.12", planned, [planned]), "v0.2.0");
    expect(run("v0.1.12", security, [security, { tag: "v0.2.0", kind: "manual" }]), undefined);
    expect(run("v0.1.12", security, [security, { tag: "v0.2.0", kind: "weekly" }, { tag: "v0.1.13", kind: "security" }, { tag: "v0.1.14", prerelease: true }]), "v0.1.13");
    expect(run("v0.1.12", security, [security]), undefined);
    expect(run("v0.1.12", security, [security, planned], { SIGNATURE_STATUS: "1" }), undefined);
    expect(run("v0.2.1", security, []), "v0.2.3");
    expect(run("v0.2.3", security, []), "");
    expect(run("v0.3.0", security, [security, planned]), undefined);
    expect(run("v0.1.12", { tag: "v0.2.4", prerelease: true }, [planned]), undefined);
    expect(run("v0.1.12", { tag: "v0.2.4-rc.1" }, [planned]), undefined);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
