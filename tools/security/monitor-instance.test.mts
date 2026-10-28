import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

test("instance monitoring fails closed without logging capabilities or retaining temporary links", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-monitor-test-"));
  try {
    const cli = join(directory, "cli");
    writeFileSync(cli, `#!${process.execPath}
import {readFileSync,writeFileSync,statSync} from 'node:fs';
const args=process.argv.slice(2), mode=process.env.TEST_MODE, state=process.env.TEST_STATE;
const fail=()=>{console.error('https://private.invalid/#SECRET_CAPABILITY');process.exit(1)};
if(args[0]==='create'){
  if(mode==='create')fail();
  if(args.slice(3).join(' ')!=='--ttl 5m --opens 1')fail();
  writeFileSync(state,readFileSync(0),{mode:0o600});
  console.log('https://private.invalid/#SECRET_CAPABILITY');
} else {
  if((statSync(args[2]).mode&0o777)!==0o600)fail();
  const marker=state+'.read';
  let again=false;try{readFileSync(marker);again=true}catch{}
  if(again&&mode!=='replay'){
    console.error(mode==='unrelated'?'https://private.invalid/#SECRET_CAPABILITY':'the message was already opened');
    process.exit(mode==='wrong-exit'?2:1);
  }
  writeFileSync(marker,'1');
  process.stdout.write(mode==='corrupt'?'wrong plaintext':readFileSync(state));
}
`);
    chmodSync(cli, 0o700);
    for (const mode of ["success", "create", "corrupt", "replay", "unrelated", "wrong-exit"]) {
      const state = join(directory, `state-${mode}`);
      const result = spawnSync(process.execPath, [resolve("tools/security/monitor-instance.mts")], {
        encoding: "utf8",
        env: { ...process.env, URL: "https://example.invalid", NYLZO_MONITOR_CLI: cli, RUNNER_TEMP: directory, TEST_MODE: mode, TEST_STATE: state },
      });
      assert.equal(result.status, mode === "success" ? 0 : 1, mode);
      assert.doesNotMatch(result.stdout + result.stderr, /SECRET_CAPABILITY|private\.invalid/u);
      assert.equal(readdirSync(directory).some(name => name.startsWith("nylzo-monitor-")), false);
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
