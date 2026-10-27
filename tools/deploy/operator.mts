import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { copyFile, lstat, mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { configuration } from "./configuration.mts";
import { profile, render, toolchain } from "./render.mts";

export async function privateFile(filename: string) {
  const information = await lstat(filename);
  assert(information.isFile() && !information.isSymbolicLink() && information.size <= 8192, "Expected a small private file");
  assert.equal(information.mode & 0o077, 0, "Credential files must have mode 0600 or stricter");
  if (process.getuid) assert.equal(information.uid, process.getuid(), "Credential file belongs to another account");
  return (await readFile(filename, "utf8")).trim();
}

export async function credential(name: string) {
  const value = process.env[name] ?? (process.env[`${name}_FILE`] ? await privateFile(process.env[`${name}_FILE`]!) : "");
  assert.match(value, /^[!-~]{20,512}$/u, `Set ${name} or its private ${name}_FILE; never paste it into chat`);
  return value;
}

export async function encryption(root: string) {
  if (process.env.TF_ENCRYPTION) return process.env.TF_ENCRYPTION;
  const passphrase = await privateFile(path.join(root, "state-encryption.key"));
  assert(passphrase.length >= 64);
  return `key_provider "pbkdf2" "operator" { passphrase = ${JSON.stringify(passphrase)} }\nmethod "aes_gcm" "operator" { keys = key_provider.pbkdf2.operator }\nstate {\nmethod = method.aes_gcm.operator\nenforced = true\n}\nplan {\nmethod = method.aes_gcm.operator\nenforced = true\n}`;
}

export function client(token: string, service: "hetzner" | "cloudflare" = "hetzner") {
  const origin = service === "hetzner" ? "https://api.hetzner.cloud/v1" : "https://api.cloudflare.com/client/v4";
  return async (pathname: string, body?: object) => {
    assert.match(pathname, /^\/[a-z0-9_/?=&.-]+$/iu);
    const response = await fetch(origin + pathname, {
      method: body ? "POST" : "GET", redirect: "error", signal: AbortSignal.timeout(30000),
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      ...(body ? { body: JSON.stringify(body) } : {}),
    });
    assert(response.ok, `${service} API returned HTTP ${response.status}; credential and response contents were omitted`);
    return response.json();
  };
}

export function execute(directory: string, environment: NodeJS.ProcessEnv, arguments_: string[], capture = false) {
  const result = execFileSync("tofu", [`-chdir=${directory}`, ...arguments_], {
    env: environment, encoding: "utf8", stdio: capture ? ["ignore", "pipe", "pipe"] : "inherit", maxBuffer: 8 * 1024 * 1024,
  });
  return capture ? result.toString() : "";
}

export async function prepare(siteFile: string, kind: string) {
  assert(["image", "server", "dns"].includes(kind));
  const site = configuration(JSON.parse(await readFile(siteFile, "utf8")));
  assert(!/(^|\.)example\.(org|com|net)$/u.test(site.domain), "Replace the example domain before provisioning");
  assert(site.administratorNetworks.every((network: string) => !/^(192\.0\.2\.|198\.51\.100\.|203\.0\.113\.|2001:db8:)/iu.test(network)), "Replace the example administrator networks before provisioning");
  assert(site.operator.length > 0 && site.contact.length > 0, "Set the public operator and contact details before provisioning");
  const root = path.dirname(path.resolve(siteFile));
  const privateDirectory = await lstat(root);
  assert(privateDirectory.isDirectory() && !privateDirectory.isSymbolicLink() && (privateDirectory.mode & 0o077) === 0, "Operator directory must be private (0700) and must not be a symlink");
  const directory = path.join(root, kind);
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const source = path.join(profile, "infrastructure", kind);
  for (const filename of await readdir(source)) {
    if (filename.endsWith(".tf") || filename === ".terraform.lock.hcl") await copyFile(path.join(source, filename), path.join(directory, filename));
  }
  const rendered = await render(site);
  const userData = kind === "image" ? rendered.cloud : JSON.stringify(rendered.ignition);
  const userDataPath = path.join(directory, "user-data");
  if (kind !== "dns") await writeFile(userDataPath, userData, { mode: 0o600 });
  let variables: object = { name: site.name, location: site.location, server_type: site.serverType, user_data_path: userDataPath };
  if (kind === "server") {
    const image = JSON.parse(await readFile(path.join(root, "image.json"), "utf8"));
    assert.equal(image.version, toolchain.flatcar);
    assert.equal(image.architecture, site.architecture);
    assert(Number.isSafeInteger(image.id) && image.id > 0);
    variables = { ...variables, image_id: image.id, flatcar_version: toolchain.flatcar, architecture: site.architecture, administrator_networks: site.administratorNetworks };
  }
  if (kind === "dns") {
    assert.match(site.cloudflareAccountId, /^[a-f0-9]{32}$/u, "Set the Cloudflare account ID in site.json");
    const outputs = JSON.parse(execute(path.join(root, "server"), { ...process.env, TF_ENCRYPTION: await encryption(root) }, ["output", "-json"], true));
    variables = { account_id: site.cloudflareAccountId, zone: site.dnsZone ?? site.domain, domain: site.domain, ipv4_address: outputs.ipv4_address.value, ipv6_address: outputs.ipv6_address.value };
  }
  await writeFile(path.join(directory, "deployment.auto.tfvars.json"), JSON.stringify(variables), { mode: 0o600 });
  const environment: NodeJS.ProcessEnv = { ...process.env, TF_IN_AUTOMATION: "true", TF_ENCRYPTION: await encryption(root) };
  for (const name of ["GH_TOKEN", "GITHUB_TOKEN", "HCLOUD_TOKEN", "CLOUDFLARE_API_TOKEN", "TF_LOG", "TF_LOG_PATH", "TF_LOG_PROVIDER", "TF_LOG_SDK"]) delete environment[name];
  if (kind === "dns") environment.CLOUDFLARE_API_TOKEN = await credential("CLOUDFLARE_API_TOKEN");
  else environment.HCLOUD_TOKEN = await credential("HCLOUD_TOKEN");
  const version = JSON.parse(execute(directory, environment, ["version", "-json"], true));
  assert.equal(version.terraform_version, toolchain.opentofu);
  return { site, root, directory, environment };
}

const hash = (contents: Uint8Array | string) => createHash("sha256").update(contents).digest("hex");
export async function inputs(directory: string) {
  const names = (await readdir(directory)).filter(name => name.endsWith(".tf") || name.endsWith(".tfvars.json") || name === "user-data" || name === ".terraform.lock.hcl").sort();
  return hash((await Promise.all(names.map(async name => `${name}:${hash(await readFile(path.join(directory, name)))}`))).join("\n"));
}

export async function waitUntil(read: () => Promise<boolean>, attempts = 120) {
  for (let attempt = 0; attempt < attempts; attempt++) {
    if (await read()) return;
    await new Promise(resolve => setTimeout(resolve, 10000));
  }
  throw new Error("Cloud operation timed out; inspect its status before retrying");
}

export async function importDns(prepared: Awaited<ReturnType<typeof prepare>>) {
  const { site, directory, environment } = prepared;
  const api = client(environment.CLOUDFLARE_API_TOKEN!, "cloudflare");
  const { result: zones } = await api(`/zones?name=${site.dnsZone ?? site.domain}`);
  assert(Array.isArray(zones) && zones.length <= 1, "Ambiguous Cloudflare zone");
  if (zones.length === 0) return;
  const zone = zones[0];
  assert.equal(zone.account.id, site.cloudflareAccountId);
  assert.match(zone.id, /^[a-f0-9]{32}$/u);
  const imports: [string, string][] = [["cloudflare_zone.zone", zone.id]];
  for (const type of ["A", "AAAA", "CAA"]) {
    const { result: records } = await api(`/zones/${zone.id}/dns_records?name=${site.domain}&type=${type}`);
    assert(Array.isArray(records));
    for (const [resource, matching] of type === "CAA"
      ? [["cloudflare_dns_record.certificate_authority", records.filter((record: any) => record.data?.tag === "issue")], ["cloudflare_dns_record.no_wildcard_certificates", records.filter((record: any) => record.data?.tag === "issuewild")]] as const
      : [[`cloudflare_dns_record.addresses["${type}"]`, records]] as const) {
      assert(matching.length <= 1, "Multiple existing address or CAA records require explicit migration review");
      if (matching.length === 1) {
        assert.match(matching[0].id, /^[a-f0-9]{32}$/u);
        imports.push([resource, `${zone.id}/${matching[0].id}`]);
      }
    }
  }
  await writeFile(path.join(directory, "existing-zone.tf"), imports.map(([resource, id]) => `import {\n  to = ${resource}\n  id = ${JSON.stringify(id)}\n}\n`).join("\n"), { mode: 0o600 });
}

export async function createImage(prepared: Awaited<ReturnType<typeof prepare>>) {
  const { root, directory, environment, site } = prepared;
  const api = client(environment.HCLOUD_TOKEN!);
  const id = Number(execute(directory, environment, ["output", "-raw", "server_id"], true));
  assert(Number.isSafeInteger(id) && id > 0);
  try {
    await waitUntil(async () => {
      const { server } = await api(`/servers/${id}`);
      assert.equal(server.labels.purpose, "image-builder");
      assert.equal(server.name, `${site.name}-image-builder`);
      assert.equal(server.server_type.architecture, site.architecture === "amd64" ? "x86" : "arm");
      return server.status === "off";
    });
    const created = await api(`/servers/${id}/actions/create_image`, {
      type: "snapshot", description: `Nylzo verified Flatcar ${toolchain.flatcar} ${site.architecture}`,
      labels: { "nylzo-verified-image": "true", "nylzo-flatcar-version": toolchain.flatcar, "nylzo-architecture": site.architecture },
    });
    assert(Number.isSafeInteger(created.image.id));
    await waitUntil(async () => {
      const { image } = await api(`/images/${created.image.id}`);
      if (image.status === "creating") return false;
      assert.equal(image.status, "available");
      assert.equal(image.type, "snapshot");
      return true;
    });
    await writeFile(path.join(root, "image.json"), JSON.stringify({ id: created.image.id, version: toolchain.flatcar, architecture: site.architecture, builder: id, created: new Date().toISOString(), signingKey: "F88CFEDEFF29A5B4D9523864E25D9AED0593B34A", factoryInputs: await inputs(directory) }), { mode: 0o600 });
    console.log(`Verified Flatcar image ${created.image.id} is ready.`);
  } finally {
    execute(directory, environment, ["destroy", "-auto-approve", "-input=false"]);
  }
}

export async function initialize(sshPublicKey: string, destination: string) {
  const root = path.resolve(destination);
  await mkdir(root, { mode: 0o700 });
  const site = JSON.parse(await readFile(path.join(profile, "site.example.json"), "utf8"));
  site.administratorPublicKey = (await readFile(sshPublicKey, "utf8")).trim();
  const keyFile = path.join(root, "report-key");
  const report = execFileSync(process.env.NYLZO_CLI ?? "nylzo", ["report-key", "--key-file", keyFile], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  assert(report.startsWith("NYLZO_REPORT_KEY="));
  site.reportPublicKey = report.trim().slice("NYLZO_REPORT_KEY=".length);
  await writeFile(path.join(root, "state-encryption.key"), randomBytes(64).toString("base64"), { mode: 0o600, flag: "wx" });
  await writeFile(path.join(root, "site.json"), JSON.stringify(site, null, 2) + "\n", { mode: 0o600, flag: "wx" });
  console.log(`Created private operator configuration in ${root}. Back up the reporting and state-encryption keys securely before provisioning.`);
}
