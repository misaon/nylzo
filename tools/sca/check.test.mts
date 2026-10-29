import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { assess, platforms } from "./check.mts";
import { deepmergePatch } from "./deepmerge.mts";

function fixture() {
  const entry = { package: { name: "golang.org/x/crypto", version: "0.58.0", ecosystem: "Go" }, licenses: ["BSD-3-Clause"], vulnerabilities: [{ id: "GO-2026-5932" }] };
  const report = { results: [{ source: { path: "/project/go.mod" }, packages: [entry] }, { source: { path: "/project/web/pnpm-lock.yaml" }, packages: [{ package: { name: "svelte", version: "5.57.2", ecosystem: "npm" }, licenses: ["MIT"] }] }, { source: { path: "/project/tools/release/package-lock.json" }, packages: [{ package: { name: "release-please", version: "17.11.2", ecosystem: "npm" }, licenses: ["Apache-2.0"] }] }] };
  const policy = { allowedLicenses: ["BSD-3-Clause", "MIT", "Apache-2.0"], notAffected: [{ id: "GO-2026-5932", ecosystem: "Go", package: "golang.org/x/crypto", version: "0.58.0", reviewUntil: "2027-01-25T00:00:00Z", forbiddenImport: "golang.org/x/crypto/openpgp", justification: "vulnerable_code_not_present", impact: "OpenPGP is absent." }] };
  const imports = Object.fromEntries(platforms.map(platform => [platform, ["fmt", "github.com/misaon/nylzo/cmd/nylzo"]]));
  return { report, policy, imports };
}

test("VEX cannot hide new versions, advisories, reachable code, missing platforms, or expired assessments", () => {
  const source = fixture(), date = new Date("2026-10-27T00:00:00Z");
  assert.equal(assess(source.report, source.policy, source.imports, date).length, 1);
  const mutations = [
    (data: any) => data.report.results[0].packages[0].package.version = "0.58.1",
    (data: any) => data.report.results[0].packages[0].vulnerabilities[0].id = "GO-another",
    (data: any) => data.report.results[0].packages[0].vulnerabilities[0].id = "MAL-2026-123",
    (data: any) => data.imports["linux/amd64"].push("golang.org/x/crypto/openpgp/packet"),
    (data: any) => delete data.imports["windows/arm64"],
    (data: any) => data.policy.notAffected[0].reviewUntil = "2026-10-26T00:00:00Z",
    (data: any) => data.report.results.pop(),
    (data: any) => data.report.results[1].packages[0].licenses = [],
    (data: any) => data.report.results[1].packages[0].licenses = ["LicenseRef-Unknown"],
  ];
  for (const mutate of mutations) { const data = structuredClone(source); mutate(data); assert.throws(() => assess(data.report, data.policy, data.imports, date)); }
});

test("development-tool license allowances do not broaden the application license policy", () => {
  const source = fixture();
  (source.policy as any).toolingAllowedLicenses = ["CC-BY-3.0"];
  source.report.results[2]!.packages[0]!.licenses = ["CC-BY-3.0"];
  assert.equal(assess(source.report, source.policy, source.imports, new Date("2026-10-28T00:00:00Z")).length, 1);
  source.report.results[1]!.packages[0]!.licenses = ["CC-BY-3.0"];
  assert.throws(() => assess(source.report, source.policy, source.imports, new Date("2026-10-28T00:00:00Z")));
});

test("a patched dependency is fixed only with exact, current installation evidence", () => {
  const policy = JSON.parse(readFileSync("security/sca-policy.json", "utf8"));
  const source = fixture(), date = new Date("2026-10-28T00:00:00Z");
  source.report.results[1]!.packages[0] = { package: { name: "deepmerge", version: "4.3.1", ecosystem: "npm" }, licenses: ["MIT"], vulnerabilities: [{ id: deepmergePatch.id }] } as any;
  const fixed = assess(source.report, policy, source.imports, date, [deepmergePatch]);
  assert.equal(fixed.find(statement => statement.vulnerability.name === deepmergePatch.id)?.status, "fixed");
  assert.throws(() => assess(source.report, policy, source.imports, date));
  assert.throws(() => assess(source.report, policy, source.imports, date, [{ ...deepmergePatch, sha256: "changed" }]));
  assert.throws(() => assess(source.report, policy, source.imports, new Date("2026-11-28T00:00:00Z"), [deepmergePatch]));
  for (const id of ["GHSA-another", "MAL-2026-123"]) {
    const changed = structuredClone(source.report);
    (changed.results[1]!.packages[0] as any).vulnerabilities[0].id = id;
    assert.throws(() => assess(changed, policy, source.imports, date, [deepmergePatch]));
  }
  const unpatchedTool = structuredClone(source.report);
  unpatchedTool.results[2]!.packages.push(structuredClone(source.report.results[1]!.packages[0]));
  assert.throws(() => assess(unpatchedTool, policy, source.imports, date, [deepmergePatch]), "A web patch cannot exempt an unpatched copy in release tooling");
  const changedPolicy = structuredClone(policy);
  changedPolicy.fixed[0].patchSha256 = "changed";
  assert.throws(() => assess(source.report, changedPolicy, source.imports, date, [deepmergePatch]));
});
