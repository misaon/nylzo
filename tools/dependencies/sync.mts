import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const assets = JSON.parse(readFileSync("tools/dependencies/assets.json", "utf8"));
const toolchain = JSON.parse(readFileSync("deploy/hetzner-flatcar/toolchain.json", "utf8"));
// min_version is a compatibility floor, not the installed Mise release pin.
// Migrate the old annotation on published baselines without raising the floor.
const floorAnnotation = /^# renovate: datasource=github-releases depName=jdx\/mise\n(?=min_version = ")/u;
const mise = readFileSync("mise.toml", "utf8").replace(floorAnnotation, "");
writeFileSync("mise.toml", mise);
for (const name of ["gh", "cosign", "jq"]) {
  const first = assets[`${name}-amd64`], second = assets[`${name}-arm64`];
  assert.equal(first.version, second.version, "Both architectures must advance together");
  toolchain[name] = { version: first.version.replace(/^(?:v|jq-)/u, ""), amd64: first.sha256, arm64: second.sha256 };
}
toolchain.opentofu = /^opentofu = "([^"]+)"$/mu.exec(mise)![1];
const caddy = /image: caddy:(\d+\.\d+\.\d+)@sha256:/u.exec(readFileSync("deploy/compose/compose.yaml", "utf8"))![1];
toolchain.caddy = caddy;
writeFileSync("deploy/hetzner-flatcar/toolchain.json", JSON.stringify(toolchain, null, 2) + "\n");
writeFileSync("mise-tasks/deploy", readFileSync("mise-tasks/deploy", "utf8").replace(/opentofu@[0-9]+\.[0-9]+\.[0-9]+/u, `opentofu@${toolchain.opentofu}`));
// Refresh the isolated runtime from the reviewed primary tool pins, without
// enabling package scripts, custom mise plugins, or repository-defined commands.
const previousRuntime = readFileSync("tools/security/runtime.toml", "utf8");
let runtime = previousRuntime.replace(floorAnnotation, "");
for (const name of ["node", "gitsign"]) runtime = runtime.replace(new RegExp(`^${name} = "[^"]+"$`, "mu"), `${name} = "${new RegExp(`^${name} = "([^"]+)"$`, "mu").exec(mise)![1]}"`);
runtime = runtime.replace(/^min_version = "[^"]+"/mu, /^min_version = "[^"]+"/mu.exec(mise)![0]);
writeFileSync("tools/security/runtime.toml", runtime);
// Raising the mise minimum alone does not change any locked executable.
const lockInputs = (source: string) => source.replace(floorAnnotation, "").replace(/^min_version = "[^"]+"\n?/mu, "");
const runtimeLock = readFileSync("tools/security/runtime.lock", "utf8");
const lockedVersions = new Map([...runtimeLock.matchAll(/^\[\[tools\.(node|gitsign)\]\]\nversion = "([^"]+)"/gmu)].map(match => [match[1]!, match[2]!]));
const locksMatch = ["node", "gitsign"].every(name => lockedVersions.get(name) === new RegExp(`^${name} = "([^"]+)"$`, "mu").exec(runtime)?.[1]);
if (process.argv.includes("--locks") && (lockInputs(runtime) !== lockInputs(previousRuntime) || !locksMatch)) {
  const environment = { ...process.env, MISE_SAFE: "1", MISE_EXPERIMENTAL: "0" };
  const directory = mkdtempSync(join(tmpdir(), "nylzo-runtime-lock-"));
  try {
    writeFileSync(join(directory, "mise.toml"), runtime);
    writeFileSync(join(directory, "mise.lock"), runtimeLock);
    execFileSync(existsSync("/opt/nylzo-dependency-tools/mise") ? "/opt/nylzo-dependency-tools/mise" : "mise", ["--cd", directory, "lock", "--platform", "linux-x64,linux-arm64,macos-x64,macos-arm64"], { env: environment, stdio: "inherit" });
    writeFileSync("tools/security/runtime.lock", readFileSync(join(directory, "mise.lock")));
  } finally { rmSync(directory, { recursive: true, force: true }); }
}
let bootstrap = readFileSync("tools/security/bootstrap-policy", "utf8");
const hash = (path: string) => createHash("sha256").update(readFileSync(path)).digest("hex");
bootstrap = bootstrap.replace(/^[a-f0-9]{64}  ([^\n]+)$/gmu, (_all, path) => `${hash(path)}  ${path}`);
writeFileSync("tools/security/bootstrap-policy", bootstrap);
const pin = hash("tools/security/bootstrap-policy");
for (const path of [".github/workflows/security-updates.yml", ".github/workflows/release.yml"]) {
  const source = readFileSync(path, "utf8").replace(/echo '[a-f0-9]{64}  tools\/security\/bootstrap-policy'/gu, `echo '${pin}  tools/security/bootstrap-policy'`);
  writeFileSync(path, source);
}
console.log("Synchronized dependency metadata and isolated policy checksums. No application source changed.");
