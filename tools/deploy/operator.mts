import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { createHash, randomBytes } from "node:crypto";
import { copyFile, lstat, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { administratorKeyIsHardware, configuration, isRecord, type Site } from "./configuration.mts";
import { profile, releaseBundle, render, toolchain } from "./render.mts";
import { privateFile } from "../security/private-file.mts";

const stacks = ["image", "server", "dns"] as const;
export type Stack = (typeof stacks)[number];

export function isStack(value: unknown): value is Stack {
  return stacks.some(stack => stack === value);
}

export async function credential(name: string) {
  const file = process.env[`${name}_FILE`];
  const value = process.env[name] ?? (file ? await privateFile(file) : "");
  assert.match(value, /^[!-~]{20,512}$/u, `Set ${name}, for example from a password manager, or its private ${name}_FILE; never paste it into chat`);
  return value;
}

export async function encryption(root: string) {
  if (process.env["TF_ENCRYPTION"]) return process.env["TF_ENCRYPTION"];
  const passphrase = await privateFile(path.join(root, "state-encryption.key"));
  assert(passphrase.length >= 64);
  return `key_provider "pbkdf2" "operator" { passphrase = ${JSON.stringify(passphrase)} }\nmethod "aes_gcm" "operator" { keys = key_provider.pbkdf2.operator }\nstate {\nmethod = method.aes_gcm.operator\nenforced = true\n}\nplan {\nmethod = method.aes_gcm.operator\nenforced = true\n}`;
}

const cloudServices = {
  hetzner: {
    origin: "https://api.hetzner.cloud/v1",
    allowed: /^\/(?:servers\/[1-9][0-9]*(?:\/actions\/create_image)?|images\/[1-9][0-9]*|images\?type=snapshot&label_selector=nylzo-verified-image%3Dtrue%2Cnylzo-flatcar-version%3D[0-9.]+%2Cnylzo-architecture%3D(?:amd64|arm64))$/u,
  },
  cloudflare: {
    origin: "https://api.cloudflare.com/client/v4",
    allowed: /^\/zones(?:\/[a-f0-9]{32}\/dns_records)?\?name=[a-z0-9._-]+(?:&type=(?:A|AAAA|CAA|CNAME|MX|TXT))?$/u,
  },
};

export function client(token: string, service: keyof typeof cloudServices = "hetzner") {
  const { origin, allowed } = cloudServices[service];
  return async (pathname: string, body?: object): Promise<unknown> => {
    if (!allowed.test(pathname) || pathname.includes("..")) throw new Error("Cloud API endpoint is not allowed");
    const endpoint = new URL(origin);
    const [resource, query = ""] = pathname.split("?");
    endpoint.pathname += resource;
    endpoint.search = query;
    const response = await fetch(endpoint, {
      method: body ? "POST" : "GET", redirect: "error", signal: AbortSignal.timeout(30000),
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      ...(body ? { body: JSON.stringify(body) } : {}),
    });
    assert(response.ok, `${service} API returned HTTP ${response.status}; credential and response contents were omitted`);
    return response.json();
  };
}

function field(value: unknown, name: string): unknown {
  if (value === null || value === undefined) throw new TypeError(`The cloud API response holds no ${name}`);
  return Reflect.get(Object(value), name);
}

const releaseAsset = (release: string, name: string) => `https://github.com/misaon/nylzo/releases/download/${release}/${name}`;

function verifyChecksums(release: string, checksums: string, bundle: string) {
  execFileSync("cosign", ["verify-blob", checksums, "--bundle", bundle,
    "--certificate-identity", `https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/${release}`,
    "--certificate-oidc-issuer", "https://token.actions.githubusercontent.com"], { stdio: ["ignore", "ignore", "inherit"], timeout: 120000 });
}

export async function releaseEvidence(release: string, architecture: string, verify = verifyChecksums) {
  const response = await fetch(releaseAsset(release, "checksums.txt"), { signal: AbortSignal.timeout(30000) });
  assert(response.ok, `Cannot read the checksums of ${release}; set initialRelease to a published Nylzo release`);
  const checksums = await response.text();
  const listed = (name: string) => checksums.split("\n").flatMap(line => {
    const [digest, file] = line.split(/\s+/u);
    return file === name && digest !== undefined && /^[a-f0-9]{64}$/u.test(digest) ? [digest] : [];
  });
  const caddy = `nylzo-caddy_${release.slice(1)}_linux_${architecture}.tar.gz`;
  assert.equal(listed(caddy).length, 1, `Release ${release} ships no Caddy build; set initialRelease to a release that lists ${caddy}`);
  const host = `nylzo-host_${release.slice(1)}.tar`;
  const [digest, ...duplicates] = listed(host);
  assert(digest !== undefined && duplicates.length === 0, `Release ${release} ships no host bundle; set initialRelease to a release that lists ${host}`);
  const signature = await fetch(releaseAsset(release, "checksums.txt.sigstore.json"), { signal: AbortSignal.timeout(30000) });
  assert(signature.ok, `Cannot read the checksum signature of ${release}`);
  const directory = await mkdtemp(path.join(tmpdir(), "nylzo-release-"));
  try {
    await writeFile(path.join(directory, "checksums.txt"), checksums);
    await writeFile(path.join(directory, "checksums.txt.sigstore.json"), Buffer.from(await signature.arrayBuffer()));
    verify(release, path.join(directory, "checksums.txt"), path.join(directory, "checksums.txt.sigstore.json"));
  } finally { await rm(directory, { recursive: true, force: true }); }
  return releaseBundle(release, digest);
}

export function execute(directory: string, environment: NodeJS.ProcessEnv, arguments_: string[], capture = false) {
  const result = execFileSync("tofu", [`-chdir=${directory}`, ...arguments_], {
    env: environment, encoding: "utf8", stdio: capture ? ["ignore", "pipe", "inherit"] : "inherit", maxBuffer: 8 * 1024 * 1024,
  });
  return capture ? result.toString() : "";
}

// CI keeps OpenTofu state in S3-compatible object storage; the operator laptop keeps it in the private directory.
export function remoteState({ site, kind }: { site: string; kind: Stack }, environment: NodeJS.ProcessEnv = process.env) {
  const bucket = environment["NYLZO_STATE_BUCKET"];
  if (bucket === undefined || bucket === "") return undefined;
  const endpoint = environment["NYLZO_STATE_ENDPOINT"] ?? "";
  const region = environment["NYLZO_STATE_REGION"] ?? "";
  assert.match(bucket, /^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/u, "NYLZO_STATE_BUCKET must be an S3 bucket name");
  assert.match(endpoint, /^https:\/\/[a-z0-9.-]{1,253}$/u, "NYLZO_STATE_ENDPOINT must be an https:// origin");
  assert.match(region, /^[a-z0-9-]{2,32}$/u, "NYLZO_STATE_REGION must name the storage region");
  return `terraform {
  backend "s3" {
    bucket                      = ${JSON.stringify(bucket)}
    key                         = ${JSON.stringify(`nylzo/${site}/${kind}.tfstate`)}
    region                      = ${JSON.stringify(region)}
    endpoints                   = { s3 = ${JSON.stringify(endpoint)} }
    use_path_style              = true
    use_lockfile                = true
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_metadata_api_check     = true
    skip_s3_checksum            = true
  }
}
`;
}

// The newest snapshot that createImage labelled for this Flatcar version and architecture.
export async function verifiedImage({ token, version, architecture }: { token: string; version: string; architecture: string }) {
  const selector = encodeURIComponent(`nylzo-verified-image=true,nylzo-flatcar-version=${version},nylzo-architecture=${architecture}`);
  const images = field(await client(token)(`/images?type=snapshot&label_selector=${selector}`), "images");
  assert(Array.isArray(images), "Hetzner returned no image list");
  const listed: unknown[] = images;
  const [newest] = listed.flatMap(image => {
    const id = field(image, "id");
    const created = field(image, "created");
    const createdAt = typeof created === "string" ? Date.parse(created) : Number.NaN;
    return field(image, "status") === "available" && typeof id === "number" && Number.isSafeInteger(id) && id > 0 && !Number.isNaN(createdAt) ? [{ id, createdAt }] : [];
  }).toSorted((left, right) => right.createdAt - left.createdAt);
  assert(newest !== undefined, `No verified Flatcar ${version} ${architecture} image exists; apply the image stack first`);
  return { id: newest.id, version, architecture };
}

async function validatedSite(siteFile: string) {
  const site = configuration(JSON.parse(await readFile(siteFile, "utf8")));
  assert(!/(^|\.)example\.(org|com|net)$/u.test(site.domain), "Replace the example domain before provisioning");
  assert(site.administratorNetworks.every(network => !/^(192\.0\.2\.|198\.51\.100\.|203\.0\.113\.|2001:db8:)/iu.test(network)), "Replace the example administrator networks before provisioning");
  assert(!(site.tang?.servers ?? []).some(server => /(^|\.)example\.(org|com|net)$/u.test(new URL(server.url).hostname)), "Replace the example Tang servers before provisioning");
  assert(site.operator.length > 0 && site.contact.length > 0, "Set the public operator and contact details before provisioning");
  return site;
}

export async function workspace(siteFile: string, kind: Stack) {
  return stackWorkspace(await validatedSite(siteFile), siteFile, kind);
}

async function stackWorkspace(site: Site, siteFile: string, kind: Stack) {
  const root = path.dirname(path.resolve(siteFile));
  const privateDirectory = await lstat(root);
  assert(privateDirectory.isDirectory() && !privateDirectory.isSymbolicLink() && (privateDirectory.mode & 0o077) === 0, "Operator directory must be private (0700) and must not be a symlink");
  const directory = path.join(root, kind);
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const source = path.join(profile, "infrastructure", kind);
  for (const filename of await readdir(source)) {
    if (filename.endsWith(".tf") || filename === ".terraform.lock.hcl") await copyFile(path.join(source, filename), path.join(directory, filename));
  }
  const backend = remoteState({ site: site.name, kind });
  if (backend) await writeFile(path.join(directory, "backend.tf"), backend, { mode: 0o600 });
  const environment: NodeJS.ProcessEnv = { ...process.env, TF_IN_AUTOMATION: "true", TF_ENCRYPTION: await encryption(root) };
  for (const name of Object.keys(environment)) {
    if (["GH_TOKEN", "GITHUB_TOKEN", "HCLOUD_TOKEN", "CLOUDFLARE_API_TOKEN"].includes(name) || name.startsWith("TF_LOG")) delete environment[name];
  }
  return { site, root, directory, environment };
}

function serverOutput(outputs: unknown, name: string) {
  const output = isRecord(outputs) ? outputs[name] : undefined;
  const value = isRecord(output) ? output["value"] : undefined;
  assert(typeof value === "string", `The server stack has no ${name} output; apply the server stack first`);
  return value;
}

export async function prepare(siteFile: string, kind: Stack, verify = verifyChecksums) {
  const site = await validatedSite(siteFile);
  assert(kind !== "server" || administratorKeyIsHardware(site.administratorPublicKey), "A production server accepts only a hardware-backed sk-ssh-ed25519@openssh.com or sk-ecdsa-sha2-nistp256@openssh.com administrator key: a security key, or a Mac Secure Enclave key as the Hetzner Flatcar guide describes");
  const bundle = kind === "dns" ? undefined : await releaseEvidence(site.initialRelease, site.architecture, verify);
  const prepared = await stackWorkspace(site, siteFile, kind);
  const { root, directory, environment } = prepared;
  const userDataPath = path.join(directory, "user-data");
  if (bundle) {
    const rendered = await render(site, bundle);
    await writeFile(userDataPath, kind === "image" ? rendered.cloud : JSON.stringify(rendered.ignition), { mode: 0o600 });
  }
  let variables: object = { name: site.name, location: site.location, server_type: site.serverType, user_data_path: userDataPath };
  if (kind === "server") {
    const imageFile = path.join(root, "image.json");
    const image: unknown = existsSync(imageFile)
      ? JSON.parse(await readFile(imageFile, "utf8"))
      : await verifiedImage({ token: await credential("HCLOUD_TOKEN"), version: toolchain.flatcar, architecture: site.architecture });
    const imageId = field(image, "id");
    assert.equal(field(image, "version"), toolchain.flatcar);
    assert.equal(field(image, "architecture"), site.architecture);
    assert(typeof imageId === "number" && Number.isSafeInteger(imageId) && imageId > 0);
    variables = { ...variables, image_id: imageId, flatcar_version: toolchain.flatcar, architecture: site.architecture, administrator_networks: site.administratorNetworks, public_access: site.publicAccess ?? false };
  }
  if (kind === "dns") {
    assert.match(site.cloudflareAccountId ?? "", /^[a-f0-9]{32}$/u, "Set the Cloudflare account ID in site.json");
    const server = path.join(root, "server");
    const serverSource = path.join(profile, "infrastructure", "server");
    // Only the configuration: output refuses a lock file that init has not installed.
    for (const filename of await readdir(serverSource)) {
      if (filename.endsWith(".tf")) await copyFile(path.join(serverSource, filename), path.join(server, filename));
    }
    const outputs: unknown = JSON.parse(execute(server, environment, ["output", "-json"], true));
    variables = { account_id: site.cloudflareAccountId, zone: site.dnsZone ?? site.domain, domain: site.domain, ipv4_address: serverOutput(outputs, "ipv4_address"), ipv6_address: serverOutput(outputs, "ipv6_address"), acme_account_uri: site.acmeAccountUri ?? "" };
  }
  await writeFile(path.join(directory, "deployment.auto.tfvars.json"), JSON.stringify(variables), { mode: 0o600 });
  if (kind === "dns") environment["CLOUDFLARE_API_TOKEN"] = await credential("CLOUDFLARE_API_TOKEN");
  else environment["HCLOUD_TOKEN"] = await credential("HCLOUD_TOKEN");
  return prepared;
}

const hash = (contents: Uint8Array | string) => createHash("sha256").update(contents).digest("hex");
export async function inputs(directory: string) {
  const names = (await readdir(directory)).filter(name => name.endsWith(".tf") || name.endsWith(".tfvars.json") || name === "user-data" || name === ".terraform.lock.hcl").toSorted();
  return hash((await Promise.all(names.map(async name => `${name}:${hash(await readFile(path.join(directory, name)))}`))).join("\n"));
}

async function waitUntil(read: () => Promise<boolean>, attempts = 120) {
  for (let attempt = 0; attempt < attempts; attempt++) {
    if (await read()) return;
    await new Promise(resolve => setTimeout(resolve, 10000));
  }
  throw new Error("Cloud operation timed out; inspect its status before retrying");
}

const recordText = (record: unknown) => String(field(record, "content") ?? "").replace(/^"|"$/gu, "");
const caaTag = (record: unknown) => {
  const data = field(record, "data");
  return data === null || data === undefined ? undefined : field(data, "tag");
};

export async function importDns(prepared: Awaited<ReturnType<typeof prepare>>) {
  const { site, directory, environment } = prepared;
  const token = environment["CLOUDFLARE_API_TOKEN"];
  assert(token, "Prepare the dns stack before importing its zone");
  const api = client(token, "cloudflare");
  const zones = field(await api(`/zones?name=${site.dnsZone ?? site.domain}`), "result");
  assert(Array.isArray(zones) && zones.length <= 1, "Ambiguous Cloudflare zone");
  if (zones.length === 0) return;
  const zone: unknown = zones[0];
  const zoneId = field(zone, "id");
  assert.equal(field(field(zone, "account"), "id"), site.cloudflareAccountId);
  assert(typeof zoneId === "string");
  assert.match(zoneId, /^[a-f0-9]{32}$/u);
  const imports: [string, string][] = [["cloudflare_zone.zone", zoneId], ["cloudflare_universal_ssl_setting.zone", zoneId]];
  const records = async (name: string, type: string): Promise<unknown[]> => {
    const result = field(await api(`/zones/${zoneId}/dns_records?name=${name}&type=${type}`), "result");
    assert(Array.isArray(result));
    return result;
  };
  const managed: [string, string, string, (record: unknown) => boolean][] = [
    ['cloudflare_dns_record.addresses["A"]', site.domain, "A", () => true],
    ['cloudflare_dns_record.addresses["AAAA"]', site.domain, "AAAA", () => true],
    ["cloudflare_dns_record.certificate_authority", site.domain, "CAA", record => caaTag(record) === "issue"],
    ["cloudflare_dns_record.no_wildcard_certificates", site.domain, "CAA", record => caaTag(record) === "issuewild"],
    ["cloudflare_dns_record.null_mail_exchanger", site.domain, "MX", () => true],
    ["cloudflare_dns_record.no_mail_senders", site.domain, "TXT", record => recordText(record).startsWith("v=spf1")],
    ["cloudflare_dns_record.reject_spoofed_mail", `_dmarc.${site.domain}`, "TXT", record => recordText(record).startsWith("v=DMARC1")],
  ];
  if ((site.dnsZone ?? site.domain) === site.domain) managed.push(["cloudflare_dns_record.www[0]", `www.${site.domain}`, "CNAME", () => true]);
  for (const [resource, name, type, selected] of managed) {
    const [existing, ...others] = (await records(name, type)).filter(selected);
    assert(others.length === 0, "Multiple existing records of one managed kind require explicit migration review");
    if (existing !== undefined) {
      const recordId = field(existing, "id");
      assert(typeof recordId === "string");
      assert.match(recordId, /^[a-f0-9]{32}$/u);
      imports.push([resource, `${zoneId}/${recordId}`]);
    }
  }
  await writeFile(path.join(directory, "existing-zone.tf"), imports.map(([resource, id]) => `import {\n  to = ${resource}\n  id = ${JSON.stringify(id)}\n}\n`).join("\n"), { mode: 0o600 });
}

export async function createImage(prepared: Awaited<ReturnType<typeof prepare>>) {
  const { root, directory, environment, site } = prepared;
  const token = environment["HCLOUD_TOKEN"];
  assert(token, "Prepare the image stack before creating its image");
  const api = client(token);
  const id = Number(execute(directory, environment, ["output", "-raw", "server_id"], true));
  assert(Number.isSafeInteger(id) && id > 0);
  try {
    await waitUntil(async () => {
      const server = field(await api(`/servers/${id}`), "server");
      assert.equal(field(field(server, "labels"), "purpose"), "image-builder");
      assert.equal(field(server, "name"), `${site.name}-image-builder`);
      assert.equal(field(field(server, "server_type"), "architecture"), site.architecture === "amd64" ? "x86" : "arm");
      return field(server, "status") === "off";
    });
    const created = await api(`/servers/${id}/actions/create_image`, {
      type: "snapshot", description: `Nylzo verified Flatcar ${toolchain.flatcar} ${site.architecture}`,
      labels: { "nylzo-verified-image": "true", "nylzo-flatcar-version": toolchain.flatcar, "nylzo-architecture": site.architecture },
    });
    const imageId = field(field(created, "image"), "id");
    assert(typeof imageId === "number" && Number.isSafeInteger(imageId) && imageId > 0);
    await waitUntil(async () => {
      const image = field(await api(`/images/${imageId}`), "image");
      if (field(image, "status") === "creating") return false;
      assert.equal(field(image, "status"), "available");
      assert.equal(field(image, "type"), "snapshot");
      return true;
    });
    await writeFile(path.join(root, "image.json"), JSON.stringify({ id: imageId, version: toolchain.flatcar, architecture: site.architecture, builder: id, created: new Date().toISOString(), signingKey: "F88CFEDEFF29A5B4D9523864E25D9AED0593B34A", factoryInputs: await inputs(directory) }), { mode: 0o600 });
    console.log(`Verified Flatcar image ${imageId} is ready.`);
  } finally {
    execute(directory, environment, ["destroy", "-auto-approve", "-input=false"]);
  }
}

export async function initialize(sshPublicKey: string, destination: string) {
  const root = path.resolve(destination);
  const checkout = path.resolve(profile, "../..");
  const relative = path.relative(checkout, root);
  if (!relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative)) {
    try { execFileSync("git", ["check-ignore", "--quiet", "--", root + path.sep], { cwd: checkout, stdio: "ignore" }); }
    catch { throw new Error("Keep operator keys outside the checkout or in an ignored directory such as .deploy"); }
  }
  await mkdir(root, { mode: 0o700 });
  const example: unknown = JSON.parse(await readFile(path.join(profile, "site.example.json"), "utf8"));
  assert(isRecord(example));
  const site = { ...example, administratorPublicKey: (await readFile(sshPublicKey, "utf8")).trim() };
  await writeFile(path.join(root, "state-encryption.key"), randomBytes(64).toString("base64"), { mode: 0o600, flag: "wx" });
  await writeFile(path.join(root, "site.json"), JSON.stringify(site, null, 2) + "\n", { mode: 0o600, flag: "wx" });
  console.log(`Created private operator configuration in ${root}. Back up the state-encryption key securely before provisioning.`);
}
