import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

test("new pre-labelled migrations free proposal slots without loops or accepting another bot", () => {
  const root = mkdtempSync(join(tmpdir(), "nylzo-continuation-"));
  try {
    mkdirSync(join(root, "bin"));
    writeFileSync(join(root, "bin/gh"), `#!/usr/bin/env node
const fs=require('node:fs'),args=process.argv;
if(args.includes('POST')){fs.appendFileSync(process.env.WRITES,fs.readFileSync(0,'utf8')+'\\n');console.log('{}');}
else console.log(JSON.stringify([JSON.parse(fs.readFileSync(process.env.PULLS,'utf8'))]));
`); chmodSync(join(root, "bin/gh"), 0o700);
    const pull = { user: { login: "nylzo-dependency-updates[bot]", id: 123456 }, head: { repo: { full_name: "misaon/nylzo" } }, labels: [{ name: "requires-attention" }] };
    const run = (pulls: any[], previous: string, mode: string) => {
      writeFileSync(join(root, "pulls"), JSON.stringify(pulls)); writeFileSync(join(root, "writes"), "");
      const result = spawnSync(process.execPath, [resolve("tools/dependencies/continue.mts")], { encoding: "utf8", env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, PULLS: join(root, "pulls"), WRITES: join(root, "writes"), PREVIOUS_BLOCKED: previous, NYLZO_UPDATE_MODE: mode, GITHUB_REPOSITORY: "misaon/nylzo", GITHUB_REF: "refs/heads/main", DEPENDENCY_APP_SLUG: "nylzo-dependency-updates", DEPENDENCY_BOT_ID: "123456" } });
      return { ...result, writes: readFileSync(join(root, "writes"), "utf8") };
    };
    const weekly = run([pull, pull, pull, pull], "0", "weekly"); assert.equal(weekly.status, 0, weekly.stderr);
    assert.deepEqual(JSON.parse(weekly.writes), { ref: "main", inputs: { mode: "weekly" } });
    const repeated = run([pull, pull, pull, pull], "4", "weekly"); assert.equal(repeated.status, 0); assert.equal(repeated.writes, "");
    const other = structuredClone(pull); other.user.id = 999;
    assert.equal(run([other], "0", "security").writes, "");
    const security = run([pull], "0", "security"); assert.equal(security.status, 0);
    assert.equal(JSON.parse(security.writes).inputs.mode, "security");
  } finally { rmSync(root, { recursive: true, force: true }); }
});
