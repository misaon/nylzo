import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { appendFileSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

// Runs without write credentials, from reviewed main, against the published tree.
const root = resolve(process.argv[2] ?? ".");
const reports = resolve(process.argv[3] ?? ".dependency-reports");
mkdirSync(reports, { recursive: true });
const affected = new Set<string>();
const policy = JSON.parse(readFileSync(`${root}/security/sca-policy.json`, "utf8"));
const sourcePath = `${reports}/source.json`;
const scan = spawnSync("osv-scanner", ["scan", "source", "--lockfile=go.mod", "--lockfile=web/pnpm-lock.yaml", "--lockfile=tools/release/package-lock.json", "--all-packages", "--all-vulns", "--format=json", `--output-file=${sourcePath}`], { cwd: root, stdio: "inherit" });
assert(scan.status === 0 || scan.status === 1, "Source scan failed; do not interpret unavailable data as clean");
const source = JSON.parse(readFileSync(sourcePath, "utf8"));
assert(Array.isArray(source.results) && source.results.length === 3);
for (const result of source.results) for (const entry of result.packages) {
  for (const vulnerability of entry.vulnerabilities ?? []) {
    const assessment = [...policy.notAffected, ...(policy.fixed ?? [])].find(item => item.id === vulnerability.id && item.package === entry.package.name && item.version === entry.package.version && Date.parse(item.reviewUntil) > Date.now());
    if (!assessment) { affected.add(entry.package.name); if (entry.package.name === "stdlib") affected.add("go"); }
  }
}
const githubPackages = new Map<string, { name: string; version: string; digest: string }>();
for (const name of readdirSync(`${root}/.github/workflows`).filter(name => /\.ya?ml$/u.test(name))) {
  const workflow = readFileSync(`${root}/.github/workflows/${name}`, "utf8");
  for (const match of workflow.matchAll(/uses: ([a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+)(?:\/[^@\s]+)?@([a-f0-9]{40}) # (v?\d+\.\d+\.\d+)/gu)) githubPackages.set(`${match[1]}@${match[3]}`, { name: match[1]!, version: match[3]!, digest: match[2]! });
}
for (const { name, version, digest } of githubPackages.values()) {
  let object = JSON.parse(execFileSync("gh", ["api", `repos/${name}/git/ref/tags/${version}`], { encoding: "utf8" })).object;
  for (let depth = 0; object.type === "tag" && depth < 3; depth++) object = JSON.parse(execFileSync("gh", ["api", `repos/${name}/git/tags/${object.sha}`], { encoding: "utf8" })).object;
  assert(object.type === "commit" && object.sha === digest, `Pinned action does not match its upstream version: ${name}@${version}`);
  const findings = JSON.parse(execFileSync("gh", ["api", `advisories?type=reviewed&ecosystem=actions&affects=${encodeURIComponent(`${name}@${version}`)}&per_page=100`], { encoding: "utf8" }));
  assert(Array.isArray(findings) && findings.length < 100);
  if (findings.some(item => !item.withdrawn_at)) affected.add(name);
}
// Grype reports from the verified release and locally verified tool downloads
// are prepared by the workflow. A missing or invalid report fails closed.
const grypeFiles = readdirSync(reports).filter(name => name.endsWith(".grype.json"));
const scanStatus = JSON.parse(readFileSync(`${reports}/scan-status.json`, "utf8"));
assert(scanStatus.format === 1 && scanStatus.complete === true && scanStatus.scans.length === 8);
assert(scanStatus.scans.every((scan: any) => [0, 2].includes(scan.exitCode) && grypeFiles.includes(scan.report)), "Release scanner errors must stop update discovery");
assert(grypeFiles.length >= 8, "Both container platforms and all archive SBOMs are required");
const toolStatus = JSON.parse(readFileSync(`${reports}/tool-scan-status.json`, "utf8"));
assert(toolStatus.format === 1 && toolStatus.inventory === true && toolStatus.scans.length >= 18);
assert(toolStatus.scans.every((scan: any) => scan.code === 0 && grypeFiles.includes(`${scan.name}.grype.json`)), "Tool scanner errors must stop update discovery");
for (const name of grypeFiles) {
  const report = JSON.parse(readFileSync(`${reports}/${name}`, "utf8"));
  assert.equal(report.descriptor.name, "grype"); assert(Array.isArray(report.matches));
  for (const match of report.matches) {
    if (/^caddy-/u.test(name)) affected.add("caddy");
    if (name === "renovate.grype.json") affected.add("ghcr.io/renovatebot/renovate");
    if (/^image-/u.test(name)) affected.add("gcr.io/distroless/static-debian13");
    if (match.artifact?.name) affected.add(match.artifact.name);
    if (match.artifact?.name === "stdlib") affected.add("go");
    if (/^tool-/u.test(name)) {
      const tool = name.replace(/^tool-/u, "").replace(/\.grype\.json$/u, "");
      affected.add(tool);
      const names: Record<string,string> = { gh: "cli/cli", cosign: "sigstore/cosign", jq: "jqlang/jq", gitsign: "sigstore/gitsign", grype: "anchore/grype", "golangci-lint": "golangci/golangci-lint", goreleaser: "goreleaser/goreleaser", actionlint: "rhysd/actionlint", zizmor: "zizmorcore/zizmor", shellcheck: "koalaman/shellcheck", "osv-scanner": "google/osv-scanner", opentofu: "opentofu/opentofu", node: "nodejs/node" };
      if (names[tool]) affected.add(names[tool]!);
    }
  }
}
const dependencies = [...affected].filter(name => /^[A-Za-z0-9@_./+-]+$/u.test(name)).sort();
assert(dependencies.length <= 500);
writeFileSync(`${reports}/update-candidates.json`, JSON.stringify({ source: process.env.NYLZO_BASE_TAG, dependencies }, null, 2) + "\n");
appendFileSync(process.env.GITHUB_OUTPUT!, `affected=${JSON.stringify(dependencies)}\n`);
console.log(`Checked source packages, action advisories, release SBOMs and installed tools; ${dependencies.length} dependency identifiers require update lookups.`);
