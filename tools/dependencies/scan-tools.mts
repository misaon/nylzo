import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { externalArtifacts, prepareExternal } from "./external.mts";

const root = resolve(process.argv[2] ?? "."), reports = resolve(process.argv[3] ?? ".dependency-reports");
const inventory = process.argv.includes("--inventory");
const candidate = process.argv.includes("--candidate");
const base = process.env.DEPENDENCY_BASE_SHA;
if (candidate) assert.match(base!, /^[a-f0-9]{40}$/u);
const baseline = (path: string): string => {
  const result = spawnSync("git", ["show", `${base}:${path}`], { cwd: root, encoding: "utf8" });
  assert.equal(result.status, 0, `Cannot read the reviewed baseline for ${path}`);
  return result.stdout;
};
const toolVersions = (text: string) => new Map([...text.split("[tools]\n")[1]!.split("\n[")[0]!.matchAll(/^([a-z0-9-]+) = "([^"]+)"$/gmu)].map(match => [match[1]!, match[2]!]));
const previous = candidate ? toolVersions(baseline("mise.toml")) : new Map<string,string>();
mkdirSync(reports, { recursive: true });
const pins = readFileSync(`${root}/mise.toml`, "utf8").split("[tools]\n")[1]!.split("\n[")[0]!;
const tools = [...pins.matchAll(/^([a-z0-9-]+) = "([^"]+)"$/gmu)].map(match => match[1]!);
assert(tools.length >= 15 && new Set(tools).size === tools.length);
const statuses: { name: string; code: number; gated: boolean; high: number }[] = [];
const environment = { ...process.env, GRYPE_CHECK_FOR_APP_UPDATE: "false", GRYPE_DB_VALIDATE_AGE: "true", GRYPE_DB_MAX_ALLOWED_BUILT_AGE: "48h", GRYPE_DB_AUTO_UPDATE: "true", GRYPE_FAIL_ON_SEVERITY: "" };
const scan = (name: string, target: string, platform?: string, changed = true) => {
  const gated = !inventory && (!candidate || changed);
  const result = spawnSync("grype", [target, ...(platform ? ["--platform", platform] : []), ...(gated ? ["--fail-on", "high"] : []), ...(name === "tool-go" ? ["--exclude", "**/src/**", "--exclude", "**/test/**"] : []), "--output", "json", "--file", `${reports}/${name}.grype.json`], { env: environment, stdio: "inherit", timeout: 180000 });
  assert(result.status === 0 || (gated && result.status === 2), `Tool vulnerability scanner failed for ${name}`);
  const report = JSON.parse(readFileSync(`${reports}/${name}.grype.json`, "utf8"));
  assert(report.descriptor.name === "grype" && Array.isArray(report.matches), `Missing complete report for ${name}`);
  statuses.push({ name, code: result.status!, gated, high: report.matches.filter((match: any) => ["High", "Critical"].includes(match.vulnerability.severity)).length });
};
for (const tool of tools) {
  const directory = execFileSync("mise", ["where", tool], { cwd: root, encoding: "utf8", timeout: 30000 }).trim();
  assert(directory.startsWith("/") && directory.length > 1);
  scan(`tool-${tool}`, `dir:${directory}`, undefined, previous.get(tool) !== toolVersions(readFileSync(`${root}/mise.toml`, "utf8")).get(tool));
}
const compose = readFileSync(`${root}/deploy/compose/compose.yaml`, "utf8");
const caddy = /image: (caddy:\d+\.\d+\.\d+@sha256:[a-f0-9]{64})/u.exec(compose)![1]!;
for (const architecture of ["amd64", "arm64"]) scan(`caddy-${architecture}`, `registry:${caddy}`, `linux/${architecture}`, !candidate || !baseline("deploy/compose/compose.yaml").includes(caddy));
const workflow = readFileSync(`${root}/.github/workflows/dependencies.yml`, "utf8");
const renovate = /renovate-image: (ghcr\.io\/renovatebot\/renovate:\d+\.\d+\.\d+@sha256:[a-f0-9]{64})/u.exec(workflow)![1]!;
scan("renovate", `registry:${renovate}`, "linux/amd64", !candidate || !baseline(".github/workflows/dependencies.yml").includes(renovate));
const external = externalArtifacts(path => readFileSync(`${root}/${path}`, "utf8"));
const oldExternal = candidate ? externalArtifacts(baseline) : [];
for (const artifact of external) {
  const directory = mkdtempSync(`${tmpdir()}/nylzo-external-`);
  try {
    await prepareExternal(artifact, directory);
    scan(artifact.name, `dir:${directory}`, undefined, !candidate || JSON.stringify(oldExternal.find(item => item.name === artifact.name)) !== JSON.stringify(artifact));
  } finally { rmSync(directory, { recursive: true, force: true }); }
}
writeFileSync(`${reports}/tool-scan-status.json`, JSON.stringify({ format: 1, inventory, candidate, scans: statuses, external: external.map(({ name, dependencies }) => ({ name, dependencies })) }, null, 2) + "\n");
assert.equal(statuses.length, tools.length + 3 + external.length);
console.log(`Scanned ${tools.length} pinned tool installations, ${external.length} verified host/provider artifacts, both Caddy platforms and the Renovate image.`);
const outstanding = statuses.filter(status => status.high > 0);
if (process.env.GITHUB_STEP_SUMMARY) {
  const { appendFileSync } = await import("node:fs");
  appendFileSync(process.env.GITHUB_STEP_SUMMARY, `## Tool vulnerability inventory\n\n${outstanding.map(status => `- ${status.name}: ${status.high} high/critical findings${status.gated ? " (update blocked)" : " (unchanged upstream tool; see report)"}`).join("\n") || "No high/critical findings."}\n\nApplication release scans remain strict on every shipped archive and image. Unchanged build tools are inventoried; dependency proposals cannot upgrade to a tool with high/critical findings.\n`);
}
if (statuses.some(status => status.code === 2)) process.exitCode = 1;
