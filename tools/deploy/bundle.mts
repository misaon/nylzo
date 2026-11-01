import assert from "node:assert/strict";
import { readdir, readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const hostDirectory = fileURLToPath(new URL("../../deploy/hetzner-flatcar/host/", import.meta.url));
const modificationTime = 1767225600;
const blockSize = 512;

export async function hostSources() {
  const sources = new Map<string, string>();
  for (const entry of await readdir(hostDirectory, { recursive: true, withFileTypes: true })) {
    if (!entry.isFile()) continue;
    const name = path.relative(hostDirectory, path.join(entry.parentPath, entry.name)).split(path.sep).join("/");
    sources.set(name, await readFile(path.join(entry.parentPath, entry.name), "utf8"));
  }
  return sources;
}

export function manifestEntries(sources: Map<string, string>) {
  const [format, ...lines] = sources.get("manifest")!.trimEnd().split("\n");
  assert.equal(format, "format 1");
  return lines.map(line => {
    const [target, source, extra] = line.split(" ");
    assert(target && source && extra === undefined, `Invalid manifest line: ${line}`);
    return { target, source };
  });
}

function octal(value: number, length: number) {
  return value.toString(8).padStart(length - 1, "0") + "\0";
}

function header(name: string, size: number, directory: boolean) {
  const block = Buffer.alloc(blockSize);
  assert(Buffer.byteLength(name) < 100, `Bundle path too long: ${name}`);
  block.write(name, 0, "utf8");
  block.write(octal(directory ? 0o755 : 0o644, 8), 100);
  block.write(octal(0, 8), 108);
  block.write(octal(0, 8), 116);
  block.write(octal(size, 12), 124);
  block.write(octal(modificationTime, 12), 136);
  block.write("        ", 148);
  block.write(directory ? "5" : "0", 156);
  block.write("ustar\0", 257);
  block.write("00", 263);
  block.write("root", 265);
  block.write("root", 297);
  const checksum = block.reduce((sum, byte) => sum + byte, 0);
  block.write(checksum.toString(8).padStart(6, "0") + "\0 ", 148);
  return block;
}

export function archive(sources: Map<string, string>) {
  const names = [...sources.keys()].sort();
  for (const name of names) assert.match(name, /^(manifest|units|[a-z]+\/[A-Za-z0-9.-]+)$/u, `Unexpected bundle path ${name}`);
  const directories = [...new Set(names.filter(name => name.includes("/")).map(name => name.split("/")[0]!))].sort();
  const parts: Buffer[] = [];
  for (const name of [...directories.map(directory => `${directory}/`), ...names].sort()) {
    if (name.endsWith("/")) {
      parts.push(header(name, 0, true));
      continue;
    }
    const contents = Buffer.from(sources.get(name)!, "utf8");
    parts.push(header(name, contents.length, false), contents, Buffer.alloc((blockSize - (contents.length % blockSize)) % blockSize));
  }
  parts.push(Buffer.alloc(blockSize * 2));
  return Buffer.concat(parts);
}

if (import.meta.main) {
  const output = process.argv[2];
  assert(output?.endsWith(".tar"), "Usage: node tools/deploy/bundle.mts OUTPUT.tar");
  await mkdir(path.dirname(path.resolve(output)), { recursive: true });
  await writeFile(output, archive(await hostSources()));
  console.log(`Wrote the host bundle to ${output}`);
}
