import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import test from "node:test";

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
