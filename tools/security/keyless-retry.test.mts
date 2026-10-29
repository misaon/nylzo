import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("fresh transparency entries can be retried while all identity claims and the retry bound remain enforced", () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-keyless-retry-"));
  try {
    mkdirSync(join(root, "bin"));
    writeFileSync(join(root, "bin/git"), `#!/usr/bin/env node
if(process.argv[2]==='rev-parse')console.log('a'.repeat(40));
if(process.argv[2]==='cat-file')console.log('tag v0.1.8\\n\\nPolicy-Commit: '+'b'.repeat(40));
`);
    writeFileSync(join(root, "bin/gitsign"), `#!/usr/bin/env node
const fs=require('node:fs'),args=process.argv;
const expected={'--certificate-identity':'https://github.com/misaon/nylzo/.github/workflows/security-updates.yml@refs/heads/main','--certificate-oidc-issuer':'https://token.actions.githubusercontent.com','--certificate-github-workflow-sha':'b'.repeat(40),'--certificate-github-workflow-trigger':'workflow_run'};
for(const [key,value]of Object.entries(expected))if(args[args.indexOf(key)+1]!==value)process.exit(3);
if(args.some(arg=>/insecure|ignore-tlog|skip-tlog/.test(arg)))process.exit(4);
const count=fs.existsSync(process.env.COUNTER)?Number(fs.readFileSync(process.env.COUNTER,'utf8')):0;
fs.writeFileSync(process.env.COUNTER,String(count+1));process.exit(count<Number(process.env.FAILURES)?1:0);
`);
    writeFileSync(join(root, "bin/sleep"), '#!/bin/sh\nprintf "%s\\n" "$1" >> "$DELAYS"\n');
    for (const name of ["git", "gitsign", "sleep"]) chmodSync(join(root, "bin", name), 0o700);
    const run = (failures: string) => {
      rmSync(join(root, "counter"), { force: true }); writeFileSync(join(root, "delays"), "");
      return spawnSync("bash", ["tools/security/verify-keyless-tag", "v0.1.8", "a".repeat(40), "workflow_run"], { encoding: "utf8", timeout: 5000, env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, GITHUB_REPOSITORY: "misaon/nylzo", COUNTER: join(root, "counter"), DELAYS: join(root, "delays"), FAILURES: failures } });
    };
    const delayed = run("5"); assert.equal(delayed.status, 0, delayed.stderr);
    assert.equal(readFileSync(join(root, "counter"), "utf8"), "6");
    assert.equal(readFileSync(join(root, "delays"), "utf8"), "2\n4\n8\n16\n32\n");
    const failed = run("100"); assert.notEqual(failed.status, 0);
    assert.equal(readFileSync(join(root, "counter"), "utf8"), "8");
    assert.equal(readFileSync(join(root, "delays"), "utf8").trim().split("\n").map(Number).reduce((sum, value) => sum + value, 0), 126);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
