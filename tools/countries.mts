import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { crc32, deflateRawSync, gunzipSync } from "node:zlib";

export const dataDirectory = fileURLToPath(new URL("../internal/server/country/data/", import.meta.url));
const tablePath = `${dataDirectory}countries.bin.gz`;
const sourcePath = `${dataDirectory}source.json`;

const unknown = "\0\0";
const ipv4Last = 0xffff_ffffn;
const ipv6Last = (1n << 128n) - 1n;
const lowBits = 64n;
const upperLast = (1n << 64n) - 1n;

type Entry = { readonly start: bigint; readonly country: string };
type Family = { readonly entries: Entry[]; rows: number; dropped: number };

export function sourceURL(month: string): string {
  return `https://download.db-ip.com/free/dbip-country-lite-${month}.csv.gz`;
}

function ipv4(text: string): bigint {
  const parts = text.split(".");
  if (parts.length !== 4 || !parts.every((part) => /^(?:0|[1-9]\d{0,2})$/u.test(part) && Number(part) <= 255)) {
    throw new Error(`${text} is not an IPv4 address`);
  }
  return parts.reduce((value, part) => (value << 8n) | BigInt(part), 0n);
}

function ipv6(text: string): bigint {
  const halves = text.split("::");
  const groups = halves.map((half) => (half === "" ? [] : half.split(":")));
  const missing = 8 - groups.flat().length;
  if (halves.length > 2 || (halves.length === 1 && missing !== 0) || (halves.length === 2 && missing < 1)) {
    throw new Error(`${text} is not an IPv6 address`);
  }
  const [head = [], tail] = groups;
  const all = tail === undefined ? head : [...head, ...Array<string>(missing).fill("0"), ...tail];
  if (!all.every((group) => /^[\da-f]{1,4}$/u.test(group))) {
    throw new Error(`${text} is not an IPv6 address`);
  }
  return all.reduce((value, group) => (value << 16n) | BigInt(`0x${group}`), 0n);
}

function append(family: Family, start: bigint, country: string): void {
  if (family.entries.at(-1)?.country !== country) {
    family.entries.push({ start, country });
  }
}

// The IPv6 table keeps the upper 64 bits; the first range that starts in a /64 decides the whole /64.
function addRange(family: Family, first: bigint, last: bigint, country: string, isIPv6: boolean): void {
  const start = isIPv6 ? (first >> lowBits) + (first & upperLast ? 1n : 0n) : first;
  const end = isIPv6 ? last >> lowBits : last;
  if (start > end) {
    family.dropped += 1;
    return;
  }
  append(family, start, country);
}

export function convert(csv: string): { ipv4: Family; ipv6: Family } {
  const families = { ipv4: { entries: [], rows: 0, dropped: 0 }, ipv6: { entries: [], rows: 0, dropped: 0 } } satisfies Record<string, Family>;
  const next = { ipv4: 0n, ipv6: 0n };
  for (const line of csv.split("\n")) {
    if (line === "") {
      continue;
    }
    const [firstText, lastText, country, extra] = line.split(",");
    if (firstText === undefined || lastText === undefined || country === undefined || extra !== undefined || !/^[A-Z]{2}$/u.test(country)) {
      throw new Error(`unexpected row ${JSON.stringify(line)}`);
    }
    const isIPv6 = firstText.includes(":");
    const key = isIPv6 ? "ipv6" : "ipv4";
    const first = isIPv6 ? ipv6(firstText) : ipv4(firstText);
    const last = isIPv6 ? ipv6(lastText) : ipv4(lastText);
    if (first < next[key] || last < first) {
      throw new Error(`row ${JSON.stringify(line)} is out of order or overlaps the previous row`);
    }
    if (first > next[key]) {
      addRange(families[key], next[key], first - 1n, unknown, isIPv6);
    }
    addRange(families[key], first, last, country === "ZZ" ? unknown : country, isIPv6);
    families[key].rows += 1;
    next[key] = last + 1n;
  }
  for (const [key, last] of [["ipv4", ipv4Last], ["ipv6", ipv6Last]] as const) {
    if (families[key].rows === 0) {
      throw new Error(`the file has no ${key} rows`);
    }
    if (next[key] <= last) {
      addRange(families[key], next[key], last, unknown, key === "ipv6");
    }
  }
  return families;
}

function varint(value: bigint, output: number[]): void {
  let rest = value;
  while (rest >= 0x80n) {
    output.push(Number(rest & 0x7fn) | 0x80);
    rest >>= 7n;
  }
  output.push(Number(rest));
}

function section(entries: readonly Entry[], output: number[]): void {
  varint(BigInt(entries.length), output);
  let previous = 0n;
  for (const entry of entries) {
    varint(entry.start - previous, output);
    previous = entry.start;
  }
  for (const entry of entries) {
    output.push(entry.country.charCodeAt(0), entry.country.charCodeAt(1));
  }
}

export function encode(families: { ipv4: Family; ipv6: Family }): Uint8Array {
  const output = [..."NZCC"].map((character) => character.charCodeAt(0));
  output.push(1);
  section(families.ipv4.entries, output);
  section(families.ipv6.entries, output);
  return Uint8Array.from(output);
}

export function gzip(data: Uint8Array): Uint8Array {
  const header = [0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 2, 0xff];
  const trailer = new DataView(new ArrayBuffer(8));
  trailer.setUint32(0, crc32(data), true);
  trailer.setUint32(4, data.length, true);
  return Buffer.concat([Uint8Array.from(header), deflateRawSync(data, { level: 9 }), new Uint8Array(trailer.buffer)]);
}

async function download(url: string): Promise<Uint8Array> {
  const response = await fetch(url, { redirect: "error", signal: AbortSignal.timeout(120_000) });
  if (!response.ok) {
    throw new Error(`${url}: HTTP ${response.status}`);
  }
  return new Uint8Array(await response.arrayBuffer());
}

type RecordedSource = { readonly month: string; readonly sha256: string };

function isRecordedSource(value: unknown): value is RecordedSource {
  return typeof value === "object" && value !== null && "month" in value && typeof value.month === "string" && "sha256" in value && typeof value.sha256 === "string";
}

function recordedHash(month: string): string | undefined {
  if (!existsSync(sourcePath)) {
    return undefined;
  }
  const recorded: unknown = JSON.parse(readFileSync(sourcePath, "utf8"));
  if (!isRecordedSource(recorded)) {
    throw new Error(`${sourcePath} does not record a month and its sha256`);
  }
  return recorded.month === month ? recorded.sha256 : undefined;
}

if (import.meta.main) {
  const month = process.argv[2] ?? "";
  if (!/^\d{4}-(?:0[1-9]|1[0-2])$/u.test(month)) {
    throw new Error("usage: bun tools/countries.mts <YYYY-MM>");
  }
  const url = sourceURL(month);
  const archive = await download(url);
  const sha256 = createHash("sha256").update(archive).digest("hex");
  console.log(`${url}\nsha256 ${sha256}`);
  const expected = recordedHash(month);
  if (expected !== undefined && expected !== sha256) {
    throw new Error(`the download does not match the sha256 ${expected} recorded in ${sourcePath}`);
  }
  const families = convert(new TextDecoder("utf-8", { fatal: true }).decode(gunzipSync(archive)));
  const table = encode(families);
  const compressed = gzip(table);
  for (const [key, family] of Object.entries(families)) {
    console.log(`${key}: ${family.rows} rows, ${family.entries.length} entries, ${family.dropped} dropped below /64`);
  }
  console.log(`table: ${table.length} bytes, gzip ${compressed.length} bytes`);
  mkdirSync(dataDirectory, { recursive: true });
  writeFileSync(tablePath, compressed);
  const source = {
    name: "DB-IP IP to Country Lite",
    month,
    url,
    sha256,
    license: "CC-BY-4.0",
    attribution: "IP Geolocation by DB-IP",
    attribution_url: "https://db-ip.com",
  };
  writeFileSync(sourcePath, `${JSON.stringify(source, null, 2)}\n`);
}
