import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

type Family = "ipv4Prefix" | "ipv6Prefix";

type Source = { readonly url: string; readonly families: readonly Family[] };

export const sources: readonly Source[] = [
  { url: "https://developers.google.com/static/crawling/ipranges/common-crawlers.json", families: ["ipv4Prefix", "ipv6Prefix"] },
  { url: "https://developers.google.com/static/crawling/ipranges/user-triggered-fetchers-google.json", families: ["ipv4Prefix", "ipv6Prefix"] },
  { url: "https://www.bing.com/toolbox/bingbot.json", families: ["ipv4Prefix"] },
  { url: "https://search.seznam.cz/ipranges/seznambot.json", families: ["ipv4Prefix", "ipv6Prefix"] },
];

export const listPath = fileURLToPath(new URL("../deploy/caddy/crawlers.json", import.meta.url));

const shortestPrefix: Record<Family, number> = { ipv4Prefix: 20, ipv6Prefix: 48 };

// A source that suddenly lists far more than the crawlers use today stops the refresh for a person to look at.
const mostPrefixes = 4000;
const mostIPv4Addresses = 65_536n;

const nonGlobalIPv4 = [
  "0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24",
  "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/3",
];

type Network = { readonly family: Family; readonly address: bigint; readonly length: number; readonly text: string };

function ipv4(text: string): bigint | undefined {
  const parts = text.split(".");
  if (parts.length !== 4 || !parts.every((part) => /^(?:0|[1-9]\d{0,2})$/u.test(part) && Number(part) <= 255)) {
    return undefined;
  }
  return parts.reduce((value, part) => (value << 8n) | BigInt(part), 0n);
}

function ipv6(text: string): bigint | undefined {
  const halves = text.split("::");
  if (halves.length > 2) {
    return undefined;
  }
  const groups = halves.map((half) => (half === "" ? [] : half.split(":")));
  const missing = 8 - groups.flat().length;
  if ((halves.length === 1 && missing !== 0) || (halves.length === 2 && missing < 1)) {
    return undefined;
  }
  const all = halves.length === 1 ? groups[0]! : [...groups[0]!, ...Array<string>(missing).fill("0"), ...groups[1]!];
  if (!all.every((group) => /^[\da-f]{1,4}$/u.test(group))) {
    return undefined;
  }
  return all.reduce((value, group) => (value << 16n) | BigInt(`0x${group}`), 0n);
}

function parseNetwork(family: Family, text: string): Network {
  const [address, length, extra] = text.toLowerCase().split("/");
  const bits = family === "ipv4Prefix" ? 32 : 128;
  const value = address === undefined ? undefined : family === "ipv4Prefix" ? ipv4(address) : ipv6(address);
  const prefixLength = length !== undefined && /^\d{1,3}$/u.test(length) ? Number(length) : Number.NaN;
  if (value === undefined || extra !== undefined || !(prefixLength >= 0 && prefixLength <= bits)) {
    throw new Error(`${text} is not an ${family === "ipv4Prefix" ? "IPv4" : "IPv6"} prefix`);
  }
  if (value & ((1n << BigInt(bits - prefixLength)) - 1n)) {
    throw new Error(`${text} is not a network address`);
  }
  if (prefixLength < shortestPrefix[family]) {
    throw new Error(`${text} is wider than /${shortestPrefix[family]}`);
  }
  return { family, address: value, length: prefixLength, text: text.toLowerCase() };
}

function contains(outer: Network, inner: Network): boolean {
  const bits = outer.family === "ipv4Prefix" ? 32 : 128;
  const shift = BigInt(bits - outer.length);
  return outer.family === inner.family && inner.length >= outer.length && inner.address >> shift === outer.address >> shift;
}

function checkGlobal(network: Network): void {
  if (network.family === "ipv4Prefix") {
    if (nonGlobalIPv4.some((reserved) => contains(parseReserved("ipv4Prefix", reserved), network))) {
      throw new Error(`${network.text} is not a public address range`);
    }
    return;
  }
  const global = { family: network.family, address: 1n << 125n, length: 3, text: "2000::/3" } as const;
  const documentation = { family: network.family, address: 0x20010db8n << 96n, length: 32, text: "2001:db8::/32" } as const;
  if (!contains(global, network) || contains(documentation, network)) {
    throw new Error(`${network.text} is not a public address range`);
  }
}

function parseReserved(family: Family, text: string): Network {
  const [address, length] = text.split("/");
  return { family, address: ipv4(address!)!, length: Number(length), text };
}

export function rangesOf(document: unknown, families: readonly Family[]): Network[] {
  const prefixes: unknown = typeof document === "object" && document !== null ? Reflect.get(document, "prefixes") : undefined;
  if (!Array.isArray(prefixes) || prefixes.length === 0) {
    throw new Error("the list has no prefixes");
  }
  const networks: Network[] = [];
  for (const entry of prefixes) {
    const keys = typeof entry === "object" && entry !== null ? Object.keys(entry) : [];
    const family = keys[0];
    if (keys.length !== 1 || (family !== "ipv4Prefix" && family !== "ipv6Prefix")) {
      throw new Error(`unexpected entry ${JSON.stringify(entry)}`);
    }
    const text: unknown = Reflect.get(entry as object, family);
    if (typeof text !== "string") {
      throw new Error(`unexpected entry ${JSON.stringify(entry)}`);
    }
    if (!families.includes(family)) {
      continue;
    }
    const network = parseNetwork(family, text);
    checkGlobal(network);
    networks.push(network);
  }
  if (networks.length === 0) {
    throw new Error("the list has no usable prefixes");
  }
  return networks;
}

export function crawlerList(documents: readonly unknown[]): { sources: string[]; prefixes: string[] } {
  const networks = documents.flatMap((document, index) => {
    const source = sources[index]!;
    try {
      return rangesOf(document, source.families);
    } catch (error) {
      throw new Error(`${source.url}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
  });
  const unique = new Map(networks.map((network) => [`${network.family}:${network.address}/${network.length}`, network]));
  const ordered = [...unique.values()].toSorted(
    (left, right) =>
      left.family.localeCompare(right.family) ||
      (left.address < right.address ? -1 : left.address > right.address ? 1 : left.length - right.length),
  );
  const ipv4Addresses = ordered
    .filter((network) => network.family === "ipv4Prefix")
    .reduce((sum, network) => sum + (1n << BigInt(32 - network.length)), 0n);
  if (ordered.length > mostPrefixes || ipv4Addresses > mostIPv4Addresses) {
    throw new Error(`the sources list ${ordered.length} prefixes and ${ipv4Addresses} IPv4 addresses, more than the ceiling`);
  }
  return { sources: sources.map((source) => source.url), prefixes: ordered.map((network) => network.text) };
}

async function download(url: string): Promise<unknown> {
  const response = await fetch(url, { redirect: "error", signal: AbortSignal.timeout(30_000) });
  if (!response.ok) {
    throw new Error(`${url}: HTTP ${response.status}`);
  }
  return response.json();
}

if (import.meta.main) {
  const documents = await Promise.all(sources.map((source) => download(source.url)));
  writeFileSync(listPath, `${JSON.stringify(crawlerList(documents), null, 2)}\n`);
}
