import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";
import { isRecord } from "./configuration.mts";

test("host reports keep every failure visible without exporting operational details", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-host-status-"));
  try {
    const bin = join(directory, "bin"), output = join(directory, "status");
    mkdirSync(bin); mkdirSync(output);
    const requiredUnits = ["nylzo.service", "caddy.service", "nylzo-ready.service", "nylzo-firewall.service", "update-engine.service", "locksmithd.service", "nylzo-update.timer"];
    const fixture = { now: 2_000_000, lastCheck: 1_999_900, checked: 1_999_800, operation: "UPDATE_STATUS_IDLE", policy: true, services: Object.fromEntries(requiredUnits.map((unit): [string, string] => [unit, "active"])), active: false, result: "success", available: "16777216", clock: "1999000", certificate: true, lifetime: 160 * 3600 };
    writeFileSync(join(directory, "caddy.env"), "NYLZO_DOMAIN=nylzo.example.org\n");
    const program = `#!${process.execPath}
import {basename} from 'node:path';import {spawnSync} from 'node:child_process';import {readFileSync} from 'node:fs';
const state=JSON.parse(process.env.STATUS_FIXTURE),args=process.argv.slice(2),name=basename(process.argv[1]);
if(name==='timeout'){process.exit(spawnSync(args[1],args.slice(2),{stdio:'inherit'}).status??1)}
if(name==='host-policy')process.exit(state.policy?0:1);
if(name==='systemctl'){
 if(args[0]==='is-active'){
  const units=args.slice(1).filter(arg=>!arg.startsWith('-'));
  process.exit(units.some(unit=>unit==='nylzo-update.service'?state.active:state.services[unit]==='active')?0:3);
 }
 console.log(args.includes('Result')?state.result:'LAST_CHECK');
}else if(name==='df')console.log('Filesystem 1024-blocks Used Available Capacity Mounted on\\n/dev/test 40000000 1000000 '+state.available+' 10% /var/lib');
else if(name==='date')console.log(args[0]==='+%s'?state.now:state.lastCheck);
else if(name==='update_engine_client')console.log('LAST_CHECKED_TIME='+state.checked+'\\nCURRENT_OP='+state.operation);
else if(name==='openssl'){
 const command=args.join(' '),stamp=(seconds)=>new Date(Date.UTC(2026,10,6)+seconds*1000).toISOString().replace('T',' ').replace(/\\.\\d+Z$/,'Z');
 if(args[0]==='s_client'){console.log('served');process.exitCode=state.certificate&&command==='s_client -connect 127.0.0.1:443 -servername nylzo.example.org -verify_hostname nylzo.example.org -verify_return_error'?0:1}
 else if(command==='x509'){const served=readFileSync(0,'utf8')==='served\\n';if(served)console.log('certificate');process.exitCode=served?0:1}
 else if(command==='x509 -noout -dateopt iso_8601 -startdate -enddate')console.log('notBefore='+stamp(0)+'\\nnotAfter='+stamp(state.lifetime));
 else process.exitCode=command==='x509 -noout -checkend 129600'?0:1;
}
`;
    for (const command of ["host-policy", "systemctl", "df", "date", "update_engine_client", "timeout", "openssl"]) {
      writeFileSync(join(bin, command), program); chmodSync(join(bin, command), 0o700);
    }
    const run = (state: typeof fixture) => {
      writeFileSync(join(directory, "clock"), state.clock);
      const result = spawnSync("bash", [resolve("deploy/hetzner-flatcar/host/monitoring/host-status")], { encoding: "utf8", timeout: 30000, env: { ...process.env, PATH: `${bin}:${process.env["PATH"]}`, STATUS_FIXTURE: JSON.stringify(state), NYLZO_HOST_STATUS_DIR: output, NYLZO_DEPLOY_STATE: directory, NYLZO_CLOCK_STATE: join(directory, "clock"), NYLZO_CADDY_ENV: join(directory, "caddy.env") } });
      assert.equal(result.status, 0, result.stderr);
      const text = readFileSync(join(output, "status.json"), "utf8");
      const report: unknown = JSON.parse(text);
      const checks = isRecord(report) ? report["checks"] : undefined;
      assert(isRecord(report) && isRecord(checks), text);
      return { text, fields: Object.keys(report).toSorted(), checks };
    };
    assert(Object.values(run(fixture).checks).every(value => value === true));
    for (const [component, mutate] of ([
      ["hostPolicy", status => status.policy = false], ["services", status => status.services = {}],
      ["diskReserve", status => status.available = "8388607"], ["diskReserve", status => status.available = "invalid"],
      ["applicationUpdates", status => status.result = "exit-code"], ["applicationUpdates", status => status.lastCheck -= 1801],
      ["applicationUpdates", status => status.lastCheck = status.now + 1],
      ["applicationUpdates", status => { status.active = true; status.lastCheck = status.now - 901; }],
      ["osUpdates", status => status.checked -= 86401], ["osUpdates", status => status.checked = status.now + 1],
      ["osUpdates", status => status.operation = "UPDATE_STATUS_REPORTING_ERROR_EVENT"], ["osUpdates", status => status.operation = "UNKNOWN"],
      ["clock", status => status.clock = String(status.now - 3601)], ["clock", status => status.clock = String(status.now + 1)], ["clock", status => status.clock = ""],
      ["certificate", status => status.certificate = false], ["certificate", status => status.lifetime = 90 * 86400],
      ["certificate", status => status.lifetime = 7 * 86400 + 1],
    ] satisfies [string, (status: typeof fixture) => unknown][])) {
      const state = structuredClone(fixture); mutate(state);
      const report = run(state);
      assert.equal(report.checks[component], false, component);
      assert.deepEqual(report.fields, ["checks", "observedAt", "version"]);
    }
    for (const unit of requiredUnits) {
      for (const unitState of ["inactive", "failed", "activating", "not-found"]) {
        const state = structuredClone(fixture);
        state.services[unit] = unitState;
        const report = run(state);
        assert.equal(report.checks["services"], false, `${unit}: ${unitState} while all other required units are active`);
        assert.equal(report.checks["applicationUpdates"], true, "Service readiness is independent of the last successful application update");
      }
    }
    writeFileSync(join(directory, "failed.json"), "private deployment diagnostic");
    const report = run(fixture);
    assert.equal(report.checks["applicationUpdates"], false);
    assert.doesNotMatch(report.text, /private deployment|\/var\/lib|16777216/u);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
