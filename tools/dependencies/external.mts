import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { mkdirSync, unlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";

export type ExternalArtifact = { name: string; dependencies: string[]; version: string; url: string; hashes: string[]; format: "raw" | "zip" | "tar"; member?: string; advisory?: { ecosystem: string; name: string } };
export const externalInputs = ["tools/dependencies/assets.json", ...["image", "server", "dns"].map(name => `deploy/hetzner-flatcar/infrastructure/${name}/.terraform.lock.hcl`)];

// Only reviewed adapters can turn a lock entry into a download URL. A new
// provider or asset family requires a source PR rather than arbitrary fetching.
export function externalArtifacts(read: (path: string) => string): ExternalArtifact[] {
  const result: ExternalArtifact[] = [];
  const repositories: Record<string,string> = { "hetznercloud/hcloud": "hetznercloud/terraform-provider-hcloud", "cloudflare/cloudflare": "cloudflare/terraform-provider-cloudflare" };
  const providers = new Map<string,{ version: string; hashes: string[] }>();
  for (const path of externalInputs.slice(1)) {
    const source = read(path);
    const entries = [...source.matchAll(/provider "registry\.opentofu\.org\/([a-z0-9-]+\/[a-z0-9-]+)" \{([^}]+)\}/gu)];
    assert(entries.length > 0 && entries.length === [...source.matchAll(/^provider /gmu)].length, `Unsupported provider lock: ${path}`);
    for (const match of entries) {
      const name = match[1]!, body = match[2]!;
      assert(repositories[name], `A new provider needs a reviewed download adapter: ${name}`);
      const version = /^\s*version\s*= "(\d+\.\d+\.\d+)"$/mu.exec(body)?.[1]; assert(version);
      const hashes = [...new Set([...body.matchAll(/"zh:([a-f0-9]{64})"/gu)].map(item => item[1]!))].sort();
      assert(hashes.length > 0, `Missing archive checksums: ${path}`);
      if (providers.has(name)) assert.deepEqual(providers.get(name), { version, hashes }, "Provider pins must agree across stacks");
      providers.set(name, { version, hashes });
    }
  }
  for (const [name, { version, hashes }] of providers) for (const architecture of ["amd64", "arm64"]) {
    const binary = name.split("/")[1]!;
    result.push({ name: `external-provider-${binary}-${architecture}`, dependencies: [name, binary], version, hashes, format: "zip", member: `terraform-provider-${binary}_v${version}`, advisory: { ecosystem: "go", name: `github.com/${repositories[name]}` },
      url: `https://github.com/${repositories[name]}/releases/download/v${version}/terraform-provider-${binary}_${version}_linux_${architecture}.zip` });
  }
  const assets = JSON.parse(read(externalInputs[0]!));
  assert.equal(Object.keys(assets).length, 14, "New host assets require an inventory adapter");
  const packages: Record<string,string> = { gh: "cli/cli", cosign: "sigstore/cosign", jq: "jqlang/jq", ignition: "coreos/ignition", syft: "anchore/syft", mise: "jdx/mise", codecov: "codecov/codecov-cli" };
  for (const [name, repository] of Object.entries(packages)) for (const architecture of ["amd64", "arm64"]) {
    const asset = assets[`${name}-${architecture}`]; assert.equal(asset.package, repository);
    assert.match(asset.version, /^(?:v|jq-)?\d+\.\d+\.\d+$/u); assert.match(asset.sha256, /^[a-f0-9]{64}$/u);
    const version = asset.version.replace(/^(?:v|jq-)/u, "");
    const filename = name === "gh" ? `gh_${version}_linux_${architecture}.tar.gz` : name === "ignition" ? `ignition-validate-${architecture === "amd64" ? "x86_64" : "aarch64"}-linux` : name === "syft" ? `syft_${version}_linux_${architecture}.tar.gz` : name === "mise" ? `mise-v${version}-linux-${architecture === "amd64" ? "x64" : "arm64"}.tar.gz` : name === "codecov" ? `codecovcli_linux${architecture === "arm64" ? "_arm64" : ""}` : `${name}-linux-${architecture}`;
    const member = name === "gh" ? `gh_${version}_linux_${architecture}/bin/gh` : name === "syft" ? "syft" : name === "mise" ? "mise/bin/mise" : undefined;
    const advisory = name === "codecov" ? { ecosystem: "pip", name: "codecov-cli" } : name === "mise" ? { ecosystem: "rust", name: "mise" } : name === "syft" ? { ecosystem: "go", name: "github.com/anchore/syft" } : undefined;
    result.push({ name: `external-${name}-${architecture}`, dependencies: [repository], version: asset.version, hashes: [asset.sha256], format: member ? "tar" : "raw", ...(member ? { member } : {}), ...(advisory ? { advisory } : {}), url: `https://github.com/${repository}/releases/download/${asset.version}/${filename}` });
  }
  assert.equal(result.length, 18); assert.equal(new Set(result.map(item => item.name)).size, result.length);
  return result;
}

export function verifyExternalBytes(artifact: ExternalArtifact, bytes: Uint8Array): void {
  assert(bytes.length > 0 && bytes.length <= 256 * 1024 * 1024, "Invalid external artifact size");
  assert(artifact.hashes.includes(createHash("sha256").update(bytes).digest("hex")), `Checksum mismatch for ${artifact.name}`);
}

export async function prepareExternal(artifact: ExternalArtifact, directory: string): Promise<string> {
  mkdirSync(directory, { recursive: true });
  const response = await fetch(artifact.url, { signal: AbortSignal.timeout(120000) });
  assert(response.ok, `Download failed for ${artifact.name}: ${response.status}`);
  const maximum = 256 * 1024 * 1024;
  assert(Number(response.headers.get("content-length") ?? 0) <= maximum, "External artifact is too large");
  const chunks: Uint8Array[] = []; let size = 0;
  assert(response.body);
  for await (const chunk of response.body) { size += chunk.length; assert(size <= maximum, "External artifact exceeds size limit"); chunks.push(chunk); }
  const bytes = Buffer.concat(chunks); verifyExternalBytes(artifact, bytes);
  const filename = artifact.member?.split("/").at(-1) ?? artifact.dependencies[0]!.split("/").at(-1)!;
  assert.match(filename, /^[a-zA-Z0-9_.-]+$/u);
  assert(filename !== "." && filename !== "..");
  const target = join(directory, filename);
  if (artifact.format === "raw") writeFileSync(target, bytes);
  else {
    const archive = join(directory, "download"); writeFileSync(archive, bytes);
    const command = artifact.format === "zip" ? "unzip" : "tar";
    const list = spawnSync(command, artifact.format === "zip" ? ["-Z1", archive] : ["-tzf", archive], { encoding: "utf8", maxBuffer: 1024 * 1024, timeout: 30000 });
    assert.equal(list.status, 0, `Cannot list verified archive ${artifact.name}`);
    const names = list.stdout.trim().split("\n").filter(name => name === artifact.member || artifact.format === "zip" && name.startsWith(artifact.member!) && /^_x\d+$/u.test(name.slice(artifact.member!.length)));
    assert.equal(names.length, 1, "Archive must contain exactly one expected executable");
    const extraction = spawnSync(command, artifact.format === "zip" ? ["-p", archive, names[0]!] : ["-xOzf", archive, names[0]!], { maxBuffer: 512 * 1024 * 1024, timeout: 30000 });
    assert.equal(extraction.status, 0, "Cannot read executable from verified archive");
    // Scan only the selected executable. Do not unpack or execute archive paths.
    assert(extraction.stdout.length > 0 && extraction.stdout.length <= 512 * 1024 * 1024);
    writeFileSync(target, extraction.stdout, { mode: 0o600 }); unlinkSync(archive);
  }
  return target;
}
