import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

test("boot health rejects missing host isolation, kernel policy, host firewall or traffic protection", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-host-policy-"));
  try {
    const values = Object.fromEntries(["kernel.dmesg_restrict=1", "kernel.kptr_restrict=2", "kernel.kexec_load_disabled=1", "kernel.io_uring_disabled=2", "kernel.sysrq=0", "dev.tty.ldisc_autoload=0", "fs.suid_dumpable=0", "net.ipv4.conf.all.rp_filter=2", "net.ipv4.conf.eth0.rp_filter=2", "net.ipv4.conf.eth0.accept_redirects=0", "net.ipv4.conf.eth0.send_redirects=0", "net.ipv6.conf.eth0.accept_redirects=0"].map(value => value.split("=")));
    const state = { selinux: "Enforcing", swaps: "", values, routes: "default via 192.0.2.1 dev eth0", qdisc: "qdisc tbf 1: root refcnt 2 rate 40Mbit burst 64Kb", options: ["name=selinux", "name=userns", "name=no-new-privileges", "name=seccomp,profile=builtin"], plugins: "io.containerd.runtime.v2 task linux/amd64 ok", firewall: "table inet nylzo {\n\tchain input {\n\t\ttype filter hook input priority filter; policy drop;" };
    const program = `#!${process.execPath}\nimport {basename} from 'node:path';\nconst state=JSON.parse(process.env.POLICY_STATE);const name=basename(process.argv[1]);const value={getenforce:state.selinux,swapon:state.swaps,sysctl:state.values[process.argv.at(-1)],ip:state.routes,tc:state.qdisc,docker:state.options.join('\\n'),ctr:state.plugins,nft:state.firewall,findmnt:state.mounts?.[process.argv.at(-1)]}[name];if(value===undefined)process.exit(1);console.log(value);\n`;
    for (const command of ["getenforce", "swapon", "sysctl", "ip", "tc", "docker", "ctr", "nft", "findmnt"]) {
      writeFileSync(join(directory, command), program);
      chmodSync(join(directory, command), 0o700);
    }
    const run = (input: any) => spawnSync("bash", [resolve("deploy/hetzner-flatcar/host/monitoring/host-policy")], { encoding: "utf8", env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, POLICY_STATE: JSON.stringify(input), NYLZO_TANG_CONFIGURATION: input.mounts ? tang : join(directory, "absent") } });
    assert.equal(run(state).status, 0);
    const changes = [
      (data: any) => data.selinux = "Permissive",
      (data: any) => data.swaps = "/dev/sda2 partition 4G 0B -2",
      (data: any) => data.values["kernel.kptr_restrict"] = "1",
      (data: any) => data.values["kernel.kexec_load_disabled"] = "0",
      (data: any) => data.values["kernel.io_uring_disabled"] = "0",
      (data: any) => data.values["kernel.sysrq"] = "16",
      (data: any) => data.values["dev.tty.ldisc_autoload"] = "1",
      (data: any) => data.values["fs.suid_dumpable"] = "2",
      (data: any) => delete data.firewall,
      (data: any) => data.firewall = data.firewall.replace("policy drop", "policy accept"),
      (data: any) => data.values["net.ipv4.conf.all.rp_filter"] = "0",
      (data: any) => data.values["net.ipv6.conf.eth0.accept_redirects"] = "1",
      (data: any) => data.qdisc = data.qdisc.replace("40Mbit", "80Mbit"),
      (data: any) => data.routes += "\ndefault via 198.51.100.1 dev eth1",
      (data: any) => data.plugins = "",
      (data: any) => data.plugins += "\nio.containerd.grpc.v1 cri - ok",
      (data: any) => data.plugins += "\nio.containerd.cri.v1 runtime linux/amd64 ok",
      (data: any) => data.plugins += "\nio.containerd.transfer.v1 local - ok",
      ...state.options.map(option => (data: any) => data.options = data.options.filter((value: string) => value !== option)),
    ];
    for (const mutate of changes) { const data = structuredClone(state); mutate(data); assert.notEqual(run(data).status, 0); }
    const tang = join(directory, "tang.json");
    writeFileSync(tang, "{}");
    const mounts = Object.fromEntries(["nylzo", "nylzo-secrets", "caddy"].map(name => [`/var/lib/${name}`, `/dev/mapper/nylzo-storage[/${name}]`]).concat([["/var/log/journal", "/dev/mapper/nylzo-storage[/journal]"]]));
    assert.equal(run({ ...state, mounts }).status, 0);
    for (const target of Object.keys(mounts)) assert.notEqual(run({ ...state, mounts: { ...mounts, [target]: "/dev/sda9" } }).status, 0, target);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
