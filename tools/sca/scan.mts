import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { assess, platforms } from "./check.mts";
import { verifyDeepmergePatch } from "./deepmerge.mts";

const directory = resolve(process.argv[2] ?? ".sca-reports");
mkdirSync(directory, { recursive: true });
const version = process.env.NYLZO_VERSION;
if (version) {
  assert.match(version, /^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/u);
  assert.equal(execFileSync("git", ["status", "--porcelain", "--untracked-files=no"], { encoding: "utf8" }).trim(), "", "Release evidence requires a clean checkout");
  if (process.env.GITHUB_ACTIONS === "true") assert.equal(process.env.GITHUB_REF_NAME, `v${version.replace(/^v/u, "")}`);
}
const policy = JSON.parse(readFileSync("security/sca-policy.json", "utf8"));
const patches = [verifyDeepmergePatch()];
const reportPath = `${directory}/source.json`;
const arguments_ = ["scan", "source", "--config=security/osv-scanner.toml", "--lockfile=go.mod", "--lockfile=web/pnpm-lock.yaml", "--lockfile=tools/release/package-lock.json", "--all-packages", "--all-vulns", `--licenses=${[...policy.allowedLicenses, ...policy.toolingAllowedLicenses].filter(license => !license.includes(" OR ")).join(",")}`];
const scan = spawnSync("osv-scanner", [...arguments_, "--format=json", `--output-file=${reportPath}`], { stdio: "inherit" });
assert(scan.status === 0 || scan.status === 1, "Dependency scanner failed");
const report = JSON.parse(readFileSync(reportPath, "utf8"));
const imports: Record<string,string[]> = {};
if (report.results.some((result: any) => result.packages.some((entry: any) => entry.vulnerabilities?.length))) {
  for (const platform of platforms) {
    const [operatingSystem, architecture] = platform.split("/");
    imports[platform] = execFileSync("go", ["list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}}", "./cmd/nylzo"], { encoding: "utf8", maxBuffer: 4 * 1024 * 1024, env: { ...process.env, CGO_ENABLED: "0", GOOS: operatingSystem, GOARCH: architecture, GOTOOLCHAIN: "local" } }).trim().split("\n");
  }
}
writeFileSync(`${directory}/go-imports.json`, JSON.stringify(imports, null, 2) + "\n");
const now = new Date();
const statements = assess(report, policy, imports, now, patches);
writeFileSync(`${directory}/patch-evidence.json`, JSON.stringify(patches, null, 2) + "\n");
const commit = execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim();
assert.match(commit, /^[0-9a-f]{40}$/u);
const product = version ? `pkg:golang/github.com/misaon/nylzo@v${version.replace(/^v/u, "")}` : `https://github.com/misaon/nylzo/tree/${commit}`;
const clean = execFileSync("git", ["status", "--porcelain", "--untracked-files=no"], { encoding: "utf8" }).trim() === "";
if (statements.length && clean) {
  writeFileSync(`${directory}/nylzo${version ? `_${version.replace(/^v/u, "")}` : ""}.openvex.json`, JSON.stringify({
    "@context": "https://openvex.dev/ns/v0.2.0",
    "@id": `https://github.com/misaon/nylzo/commit/${commit}#vex-${version ?? "source"}`,
    author: "https://github.com/misaon/nylzo", timestamp: now.toISOString(), version: 1,
    statements: statements.map(({ component, ...statement }) => ({ ...statement, products: [{ "@id": product, subcomponents: [{ "@id": component }] }] })),
  }, null, 2) + "\n");
}
if (process.argv.includes("--sbom")) {
  const identity = version ? version.replace(/^v/u, "") : commit;
  const inventory = spawnSync("osv-scanner", [...arguments_, "--format=spdx-2-3", `--output-file=${directory}/nylzo_${identity}.source.spdx.json`], { stdio: "inherit" });
  assert(inventory.status === 0 || inventory.status === 1, "Source SBOM generation failed");
  const document = JSON.parse(readFileSync(`${directory}/nylzo_${identity}.source.spdx.json`, "utf8"));
  assert.equal(document.spdxVersion, "SPDX-2.3");
  assert(document.packages.length >= report.results.reduce((total: number, result: any) => total + result.packages.length, 0));
}
console.log(`Checked ${report.results.reduce((total: number, result: any) => total + result.packages.length, 0)} locked packages; ${statements.length} exact VEX assessment(s).`);
