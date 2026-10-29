import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("release/controller events are retained and the actionlint compatibility allowance stays narrow", () => {
  const config = readFileSync(".github/actionlint.yaml", "utf8");
  for (const name of ["release", "security-updates", "dependencies"]) {
    const path = `.github/workflows/${name}.yml`, source = readFileSync(path, "utf8");
    const block = /^concurrency:\n((?:  .+\n)+)/mu.exec(source)![1]!;
    assert.match(block, /^  cancel-in-progress: false$/mu);
    assert.match(block, /^  queue: max$/mu);
    assert.equal([...source.matchAll(/^\s*queue:/gmu)].length, 1, "Additional queue properties require explicit validation");
    assert(config.includes(`  ${path}:\n`));
  }
  assert.equal([...config.matchAll(/unexpected key "queue"/gu)].length, 3);
  assert(!config.includes('syntax-check') && !config.includes('workflows/**'));
  const ci = readFileSync(".github/workflows/ci.yml", "utf8");
  assert(ci.includes("group: ci-${{ github.ref }}-${{ github.event_name == 'pull_request' && 'pr' || github.sha }}"));
});

test("the release lock is held through pending publication and rejects failed or mutable results", () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-publication-"));
  try {
    mkdirSync(join(root, "bin"));
    writeFileSync(join(root, "bin/gh"), `#!/usr/bin/env node
const fs=require('node:fs'),path=process.argv[3];
if(path.includes('/actions/')){
 const count=fs.existsSync(process.env.COUNTER)?Number(fs.readFileSync(process.env.COUNTER,'utf8')):0;fs.writeFileSync(process.env.COUNTER,String(count+1));
 console.log(JSON.stringify({workflow_runs:[{id:1,event:'push',head_branch:'v0.1.7',head_sha:'a'.repeat(40),status:count===0?'queued':'completed',conclusion:process.env.CONCLUSION}]}));
}else console.log(JSON.stringify({tag_name:'v0.1.7',immutable:process.env.IMMUTABLE==='true',draft:false,prerelease:false}));
`);
    writeFileSync(join(root, "bin/sleep"), '#!/bin/sh\nexit 0\n');
    writeFileSync(join(root, "bin/timeout"), '#!/bin/sh\nshift 2\nexec "$@"\n');
    for (const name of ["gh", "sleep", "timeout"]) chmodSync(join(root, "bin", name), 0o700);
    const run = (conclusion: string, immutable = "true") => {
      rmSync(join(root, "counter"), { force: true });
      return spawnSync("bash", ["tools/release/wait-publication", "v0.1.7", "a".repeat(40)], { encoding: "utf8", timeout: 5000, env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, COUNTER: join(root, "counter"), GITHUB_REPOSITORY: "misaon/nylzo", CONCLUSION: conclusion, IMMUTABLE: immutable } });
    };
    const result = run("success"); assert.equal(result.status, 0, result.stderr);
    assert.equal(readFileSync(join(root, "counter"), "utf8"), "2");
    assert.notEqual(run("failure").status, 0);
    assert.notEqual(run("success", "false").status, 0);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
