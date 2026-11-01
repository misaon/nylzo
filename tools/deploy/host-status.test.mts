import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

test("host reports keep every failure visible without exporting operational details", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-host-status-"));
  try {
    const bin = join(directory, "bin"), output = join(directory, "status");
    mkdirSync(bin); mkdirSync(output);
    const fixture = { now: 2_000_000, lastCheck: 1_999_900, checked: 1_999_800, operation: "UPDATE_STATUS_IDLE", policy: true, services: true, active: false, result: "success", available: "16777216", clock: "1999000", certificate: true };
    const certificates = join(directory, "certificates", "acme-v02.api.letsencrypt.org-directory", "nylzo.example.org");
    mkdirSync(certificates, { recursive: true });
    writeFileSync(join(certificates, "nylzo.example.org.crt"), "certificate");
    writeFileSync(join(directory, "caddy.env"), "NYLZO_DOMAIN=nylzo.example.org\n");
    const program = `#!${process.execPath}
import {basename} from 'node:path';import {spawnSync} from 'node:child_process';
const state=JSON.parse(process.env.STATUS_FIXTURE),args=process.argv.slice(2),name=basename(process.argv[1]);
if(name==='timeout'){process.exit(spawnSync(args[1],args.slice(2),{stdio:'inherit'}).status??1)}
if(name==='host-policy')process.exit(state.policy?0:1);
if(name==='systemctl'){
 if(args[0]==='is-active')process.exit(args.at(-1)==='nylzo-update.service'?(state.active?0:3):(state.services?0:3));
 console.log(args.includes('Result')?state.result:'LAST_CHECK');
}else if(name==='df')console.log('Filesystem 1024-blocks Used Available Capacity Mounted on\\n/dev/test 40000000 1000000 '+state.available+' 10% /var/lib');
else if(name==='date')console.log(args[0]==='+%s'?state.now:state.lastCheck);
else if(name==='update_engine_client')console.log('LAST_CHECKED_TIME='+state.checked+'\\nCURRENT_OP='+state.operation);
else if(name==='openssl')process.exit(state.certificate&&args.join(' ').includes('-checkend 129600')?0:1);
`;
    for (const command of ["host-policy", "systemctl", "df", "date", "update_engine_client", "timeout", "openssl"]) {
      writeFileSync(join(bin, command), program); chmodSync(join(bin, command), 0o700);
    }
    const run = (state: any) => {
      writeFileSync(join(directory, "clock"), state.clock);
      const result = spawnSync("bash", [resolve("deploy/hetzner-flatcar/host/monitoring/host-status")], { encoding: "utf8", timeout: 30000, env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, STATUS_FIXTURE: JSON.stringify(state), NYLZO_HOST_STATUS_DIR: output, NYLZO_DEPLOY_STATE: directory, NYLZO_CLOCK_STATE: join(directory, "clock"), NYLZO_CADDY_ENV: join(directory, "caddy.env"), NYLZO_CERTIFICATES: join(directory, "certificates") } });
      assert.equal(result.status, 0, result.stderr);
      return JSON.parse(readFileSync(join(output, "status.json"), "utf8"));
    };
    assert(Object.values(run(fixture).checks).every(value => value === true));
    for (const [component, mutate] of [
      ["hostPolicy", (s: any) => s.policy = false], ["services", (s: any) => s.services = false],
      ["diskReserve", (s: any) => s.available = "8388607"], ["diskReserve", (s: any) => s.available = "invalid"],
      ["applicationUpdates", (s: any) => s.result = "exit-code"], ["applicationUpdates", (s: any) => s.lastCheck -= 1801],
      ["applicationUpdates", (s: any) => s.lastCheck = s.now + 1],
      ["applicationUpdates", (s: any) => { s.active = true; s.lastCheck = s.now - 901; }],
      ["osUpdates", (s: any) => s.checked -= 86401], ["osUpdates", (s: any) => s.checked = s.now + 1],
      ["osUpdates", (s: any) => s.operation = "UPDATE_STATUS_REPORTING_ERROR_EVENT"], ["osUpdates", (s: any) => s.operation = "UNKNOWN"],
      ["clock", (s: any) => s.clock = String(s.now - 3601)], ["clock", (s: any) => s.clock = String(s.now + 1)], ["clock", (s: any) => s.clock = ""],
      ["certificate", (s: any) => s.certificate = false],
    ] as const) {
      const state = structuredClone(fixture); mutate(state);
      const report = run(state);
      assert.equal(report.checks[component], false, component);
      assert.deepEqual(Object.keys(report).sort(), ["checks", "observedAt", "version"]);
    }
    writeFileSync(join(directory, "failed.json"), "private deployment diagnostic");
    const report = run(fixture);
    assert.equal(report.checks.applicationUpdates, false);
    assert.doesNotMatch(JSON.stringify(report), /private deployment|\/var\/lib|16777216/u);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
