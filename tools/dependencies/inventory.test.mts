import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { createRequire } from "node:module";
import test from "node:test";
import { validateDependencyFile } from "./policy.mts";

test("canonical tool pins and generated deployment/signing metadata cannot drift", () => {
  const mise = readFileSync("mise.toml", "utf8");
  const version = (tool: string) => new RegExp(`^${tool} = "([^"]+)"$`, "mu").exec(mise)![1]!;
  const assets = JSON.parse(readFileSync("tools/dependencies/assets.json", "utf8"));
  const host = JSON.parse(readFileSync("deploy/hetzner-flatcar/toolchain.json", "utf8"));
  for (const tool of ["gh", "cosign", "jq"]) {
    assert.equal(host[tool].version, version(tool));
    for (const architecture of ["amd64", "arm64"]) {
      assert.equal(assets[`${tool}-${architecture}`].version.replace(/^(?:v|jq-)/u, ""), version(tool));
      assert.equal(assets[`${tool}-${architecture}`].sha256, host[tool][architecture]);
    }
  }
  for (const [name, repository] of [["syft", "anchore/syft"], ["mise", "jdx/mise"], ["codecov", "codecov/codecov-cli"]]) {
    const expected = assets[`${name}-amd64`].version.replace(/^v/u, "");
    assert.equal(assets[`${name}-arm64`].version.replace(/^v/u, ""), expected);
    const sources = [mise, ...readdirSync(".github/workflows").map(path => readFileSync(`.github/workflows/${path}`, "utf8"))].join("\n");
    const pattern = new RegExp(`# renovate: datasource=github-releases depName=${repository!.replaceAll("/", "\\/")}\\n[ \\t]*[a-z_-]+[ \\t]*[:=][ \\t]*[\"']?(v?\\d+\\.\\d+\\.\\d+)`, "gu");
    const pins = [...sources.matchAll(pattern)]; assert(pins.length > 0);
    for (const pin of pins) assert.equal(pin[1]!.replace(/^v/u, ""), expected, `${name} workflow and checksum pins must agree`);
  }
  assert.equal(host.opentofu, version("opentofu"));
  assert(readFileSync("mise-tasks/deploy", "utf8").includes(`mise exec opentofu@${host.opentofu}`));
  for (const module of ["dns", "image", "server"]) assert(readFileSync(`deploy/hetzner-flatcar/infrastructure/${module}/main.tf`, "utf8").includes(`required_version = "= ${host.opentofu}"`));
  const runtime = readFileSync("tools/security/runtime.toml", "utf8");
  for (const tool of ["node", "gitsign"]) assert(runtime.includes(`${tool} = "${version(tool)}"`));
  const caddy = /image: caddy:(\d+\.\d+\.\d+)@sha256:/u.exec(readFileSync("deploy/compose/compose.yaml", "utf8"))![1];
  assert.equal(host.caddy, caddy);
  const config = JSON.parse(readFileSync("renovate.json", "utf8"));
  assert(config.postUpgradeTasks.fileFilters.includes("mise-tasks/deploy"));
});

test("security discovery does not re-enable duplicate CLI inputs or disable Action SHA updates", () => {
  const saved = { ...process.env };
  try {
    Object.assign(process.env, { NYLZO_UPDATE_MODE: "security", NYLZO_BASE_TAG: "v0.1.8", NYLZO_AFFECTED_DEPENDENCIES: JSON.stringify(["sigstore/cosign", "jdx/mise", "ghcr.io/renovatebot/renovate", "goreleaser/goreleaser"]) });
    const require = createRequire(import.meta.url);
    const config = require("../../.github/renovate.cjs");
    const rules = config.force.packageRules;
    const reenable = rules.findIndex((rule: any) => rule.enabled === true && rule.matchPackageNames?.includes("sigstore/cosign"));
    const exclusion = rules.findLastIndex((rule: any) => rule.enabled === false && rule.matchManagers?.includes("github-actions") && rule.matchDepTypes?.includes("uses-with"));
    assert(exclusion > reenable);
    assert.deepEqual(rules[exclusion].matchPackageNames, ["goreleaser/goreleaser", "ghcr.io/renovatebot/renovate", "sigstore/cosign", "jdx/mise"]);
    assert(!rules[exclusion].matchPackageNames.includes("jdx/mise-action"));
    assert(!rules[exclusion].matchPackageNames.includes("sigstore/cosign-installer"));
  } finally { process.env = saved; }
});

test("the Ubuntu test image follows LTS lines instead of short-lived interim releases", () => {
  const config = JSON.parse(readFileSync("renovate.json", "utf8"));
  const rule = config.packageRules.find((rule: any) => rule.matchPackageNames?.includes("ubuntu"));
  const allowed = new RegExp(rule.allowedVersions.slice(1, -1));
  for (const tag of ["24.04", "26.04", "28.04"]) assert(allowed.test(tag));
  for (const tag of ["24.10", "25.04", "26.10", "latest"]) assert(!allowed.test(tag));
  const old = `const image = "ubuntu:24.04@sha256:${"a".repeat(64)}";`;
  // The proposal filter and the independent merge policy both keep OS changes manual.
  assert.equal(rule.matchDatasources[0], "docker");
  validateDependencyFile("tools/deploy/runtime.test.mts", old, old.replace("a".repeat(64), "b".repeat(64)));
  assert.throws(() => validateDependencyFile("tools/deploy/runtime.test.mts", old, old.replace("24.04", "26.04")));
});
