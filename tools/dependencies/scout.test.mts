import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

test("six-hour discovery covers main-only dependencies and rejects incomplete inventories", () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-scout-"));
  try {
    const published = join(root, "published"), reports = join(root, "reports"), bin = join(root, "bin");
    for (const directory of [reports, bin, root, published]) mkdirSync(directory, { recursive: true });
    for (const [index, source] of [published, root].entries()) {
      for (const directory of ["web", "tools/release", "security", ".github/workflows"]) mkdirSync(join(source, directory), { recursive: true });
      for (const file of ["go.mod", "web/pnpm-lock.yaml", "tools/release/package-lock.json"]) writeFileSync(join(source, file), String(index));
      writeFileSync(join(source, "security/sca-policy.json"), '{"notAffected":[],"fixed":[]}');
      writeFileSync(join(source, ".github/workflows/ci.yml"), `uses: example/action@${String(index + 1).repeat(40)} # v1.2.${index + 3}\n`);
    }
    const report = JSON.stringify({ descriptor: { name: "grype" }, matches: [] });
    const releases = Array.from({ length: 8 }, (_, index) => ({ report: `archive-${index}.grype.json`, exitCode: 0 }));
    for (const scan of releases) writeFileSync(join(reports, scan.report), report);
    writeFileSync(join(reports, "scan-status.json"), JSON.stringify({ format: 1, complete: true, scans: releases }));
    const tools = Array.from({ length: 18 }, (_, index) => ({ name: `tool-${index}`, code: 0 }));
    for (const scan of tools) writeFileSync(join(reports, `${scan.name}.grype.json`), report);
    writeFileSync(join(reports, "tool-scan-status.json"), JSON.stringify({ format: 1, inventory: true, scans: tools }));
    writeFileSync(join(bin, "osv-scanner"), `#!/usr/bin/env node
const fs=require('node:fs'),main=!process.cwd().endsWith('/published');
const entry={package:{name:'main-only',version:'1.2.4'},vulnerabilities:main?[{id:'GHSA-test'}]:[]};
const results=['go.mod','web/pnpm-lock.yaml','tools/release/package-lock.json'].map(path=>({source:{path:process.cwd()+'/'+path},packages:[entry]}));
fs.writeFileSync(process.argv.find(arg=>arg.startsWith('--output-file=')).slice(14),JSON.stringify({results}));
`);
    writeFileSync(join(bin, "gh"), `#!/usr/bin/env node
const path=process.argv.find(arg=>arg.startsWith('repos/')||arg.startsWith('advisories?'));
if(path.includes('/git/ref/'))console.log(JSON.stringify({object:{type:'commit',sha:(path.endsWith('v1.2.3')?'1':'2').repeat(40)}}));
else console.log(JSON.stringify(path.includes('%401.2.4')||path.includes('%40v1.2.4')?[{withdrawn_at:null}]:[]));
`);
    for (const executable of ["gh", "osv-scanner"]) chmodSync(join(bin, executable), 0o700);
    const run = () => {
      writeFileSync(join(root, "output"), "");
      return spawnSync(process.execPath, [resolve("tools/dependencies/scout.mts"), published, reports], { cwd: root, encoding: "utf8", env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, GITHUB_OUTPUT: join(root, "output"), NYLZO_BASE_TAG: "v0.1.6" } });
    };
    const result = run(); assert.equal(result.status, 0, result.stderr);
    const affected = JSON.parse(readFileSync(join(root, "output"), "utf8").trim().slice("affected=".length));
    assert.deepEqual(affected, ["example/action", "main-only"]);
    assert(readFileSync(join(reports, "main-source.json"), "utf8").includes("main-only"));
    rmSync(join(reports, "tool-17.grype.json"));
    assert.notEqual(run().status, 0, "Missing reports cannot be interpreted as a clean result");
    assert.equal(readFileSync(join(root, "output"), "utf8"), "");
  } finally { rmSync(root, { recursive: true, force: true }); }
});
