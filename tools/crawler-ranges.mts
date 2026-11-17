import { appendFileSync, readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

type Family = "ipv4Prefix" | "ipv6Prefix";

type Source = { readonly url: string; readonly families: readonly Family[] };

const both: readonly Family[] = ["ipv4Prefix", "ipv6Prefix"];

export const sources: readonly Source[] = [
  { url: "https://developers.google.com/static/crawling/ipranges/common-crawlers.json", families: both },
  { url: "https://developers.google.com/static/crawling/ipranges/special-crawlers.json", families: both },
  { url: "https://developers.google.com/static/crawling/ipranges/user-triggered-fetchers-google.json", families: both },
  { url: "https://www.bing.com/toolbox/bingbot.json", families: ["ipv4Prefix"] },
  { url: "https://search.seznam.cz/ipranges/seznambot.json", families: both },
  { url: "https://openai.com/searchbot.json", families: both },
  { url: "https://openai.com/chatgpt-user.json", families: both },
  { url: "https://openai.com/gptbot.json", families: both },
  { url: "https://www.perplexity.ai/perplexitybot.json", families: both },
  { url: "https://www.perplexity.ai/perplexity-user.json", families: both },
  { url: "https://claude.com/crawling/bots.json", families: both },
  { url: "https://search.developer.apple.com/applebot.json", families: both },
  { url: "https://duckduckgo.com/duckduckbot.json", families: both },
  { url: "https://duckduckgo.com/duckassistbot.json", families: both },
  { url: "https://index.commoncrawl.org/ccbot.json", families: both },
  { url: "https://mistral.ai/mistralai-index-ips.json", families: both },
  { url: "https://mistral.ai/mistralai-user-ips.json", families: both },
];

// A shared Microsoft block that the ChatGPT-User list names, far wider than a crawler needs; left out of every source.
const leftOut: readonly string[] = ["9.129.0.0/16"];

export const listPath = fileURLToPath(new URL("../deploy/caddy/crawlers.json", import.meta.url));

const shortestPrefix: Record<Family, number> = { ipv4Prefix: 20, ipv6Prefix: 48 };

// A source that suddenly lists far more than the crawlers use today stops the refresh for a person to look at.
const mostPrefixes = 8000;
const mostIPv4Addresses = 131_072n;
// One that suddenly lists far fewer, such as a truncated download, would take crawlers off the list.
const fewestShareInTenths = 7n;

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

function parseNetwork(family: Family, text: string, shortest = shortestPrefix[family]): Network {
  const [address, length, extra] = text.toLowerCase().split("/");
  const bits = family === "ipv4Prefix" ? 32 : 128;
  const value = address === undefined ? undefined : family === "ipv4Prefix" ? ipv4(address) : ipv6(address);
  const prefixLength = length !== undefined && /^\d{1,3}$/u.test(length) ? Number(length) : Number.NaN;
  if (value === undefined || extra !== undefined || !(prefixLength >= 0 && prefixLength <= bits)) {
    throw new Error(`${JSON.stringify(text)} is not an ${family === "ipv4Prefix" ? "IPv4" : "IPv6"} prefix`);
  }
  if (value & ((1n << BigInt(bits - prefixLength)) - 1n)) {
    throw new Error(`${text} is not a network address`);
  }
  if (prefixLength < shortest) {
    throw new Error(`${text} is wider than /${shortest}`);
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
  // Teredo and 6to4 addresses carry an arbitrary IPv4 host inside them.
  const special = [
    { family: network.family, address: 0x20010db8n << 96n, length: 32, text: "2001:db8::/32" },
    { family: network.family, address: 0x20010000n << 96n, length: 32, text: "2001::/32" },
    { family: network.family, address: 0x2002n << 112n, length: 16, text: "2002::/16" },
  ] as const;
  if (!contains(global, network) || special.some((reserved) => contains(reserved, network))) {
    throw new Error(`${network.text} is not a public address range`);
  }
}

function parseReserved(family: Family, text: string): Network {
  const [address, length] = text.split("/");
  return { family, address: ipv4(address!)!, length: Number(length), text };
}

export function rangesOf(document: unknown, families: readonly Family[], dropped: string[] = []): Network[] {
  const prefixes: unknown = typeof document === "object" && document !== null ? Reflect.get(document, "prefixes") : undefined;
  if (!Array.isArray(prefixes) || prefixes.length === 0) {
    throw new Error("the list has no prefixes");
  }
  const excluded = leftOut.map((text) => parseNetwork(text.includes(":") ? "ipv6Prefix" : "ipv4Prefix", text, 0));
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
    const candidate = parseNetwork(family, text, 0);
    if (excluded.some((outer) => contains(outer, candidate))) {
      dropped.push(candidate.text);
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

export type CrawlerList = { readonly sources: Readonly<Record<string, readonly string[]>>; readonly prefixes: readonly string[] };

export type Download = { readonly document: unknown } | { readonly failure: string };

export type Kept = { readonly url: string; readonly failure: string };

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function ordered(networks: readonly Network[]): Network[] {
  const unique = new Map(networks.map((network) => [`${network.family}:${network.address}/${network.length}`, network]));
  return [...unique.values()].toSorted(
    (left, right) =>
      left.family.localeCompare(right.family) ||
      (left.address < right.address ? -1 : left.address > right.address ? 1 : left.length - right.length),
  );
}

function asDocument(prefixes: readonly string[]): unknown {
  return { prefixes: prefixes.map((prefix) => (prefix.includes(":") ? { ipv6Prefix: prefix } : { ipv4Prefix: prefix })) };
}

function committedRanges(committed: CrawlerList, url: string): readonly string[] | undefined {
  return Object.hasOwn(committed.sources, url) ? committed.sources[url] : undefined;
}

// A source that does not download keeps its committed ranges, so one vendor's outage never holds back the others.
export function crawlerList(
  downloads: readonly Download[],
  committed: CrawlerList,
  dropped: string[] = [],
): { list: CrawlerList; kept: Kept[] } {
  const kept: Kept[] = [];
  const perSource = sources.map((source, index) => {
    const download = downloads[index]!;
    try {
      if ("document" in download) {
        return ordered(rangesOf(download.document, source.families, dropped));
      }
      const previous = committedRanges(committed, source.url);
      if (previous === undefined) {
        throw new Error(`${download.failure}, and the committed list holds no ranges of it to keep`);
      }
      kept.push({ url: source.url, failure: download.failure });
      return ordered(rangesOf(asDocument(previous), source.families, dropped));
    } catch (error) {
      throw new Error(`${source.url}: ${message(error)}`, { cause: error });
    }
  });
  const union = ordered(perSource.flat());
  const ipv4Addresses = union
    .filter((network) => network.family === "ipv4Prefix")
    .reduce((sum, network) => sum + (1n << BigInt(32 - network.length)), 0n);
  if (union.length > mostPrefixes || ipv4Addresses > mostIPv4Addresses) {
    throw new Error(`the sources list ${union.length} prefixes and ${ipv4Addresses} IPv4 addresses, more than the ceiling`);
  }
  const list = {
    sources: Object.fromEntries(sources.map((source, index) => [source.url, perSource[index]!.map((network) => network.text)])),
    prefixes: union.map((network) => network.text),
  };
  return { list, kept };
}

export function readCrawlerList(text: string): CrawlerList {
  const value: unknown = JSON.parse(text);
  const strings = (entry: unknown): entry is string[] => Array.isArray(entry) && entry.every((item) => typeof item === "string");
  const listed = typeof value === "object" && value !== null ? value : {};
  const ranges: unknown = Reflect.get(listed, "sources");
  const prefixes: unknown = Reflect.get(listed, "prefixes");
  const bySource = typeof ranges === "object" && ranges !== null && !Array.isArray(ranges) && Object.values(ranges).every(strings);
  if (!bySource || !strings(prefixes)) {
    throw new Error(`${listPath} does not hold the ranges of each source and their union`);
  }
  return { sources: ranges as Record<string, string[]>, prefixes };
}

function coverage(prefixes: readonly string[]): { prefixes: bigint; ipv4Addresses: bigint } {
  const ipv4Addresses = prefixes
    .filter((prefix) => !prefix.includes(":"))
    .reduce((sum, prefix) => sum + (1n << BigInt(32 - parseNetwork("ipv4Prefix", prefix).length)), 0n);
  return { prefixes: BigInt(prefixes.length), ipv4Addresses };
}

function shrunk(subject: string, committed: readonly string[], refreshed: readonly string[]): string[] {
  const before = coverage(committed);
  const after = coverage(refreshed);
  const kept = (share: bigint, whole: bigint) => share * 10n >= whole * fewestShareInTenths;
  if (kept(after.prefixes, before.prefixes) && kept(after.ipv4Addresses, before.ipv4Addresses)) {
    return [];
  }
  return [
    `${subject} ${after.prefixes} prefixes and ${after.ipv4Addresses} IPv4 addresses, ` +
      `far fewer than the ${before.prefixes} and ${before.ipv4Addresses} of the committed list`,
  ];
}

// Checked per source too, because a truncated small list hardly moves the total.
export function shrinkage(committed: CrawlerList, refreshed: CrawlerList): string[] {
  return [
    ...sources.flatMap(({ url }) => {
      const before = committedRanges(committed, url);
      const after = committedRanges(refreshed, url);
      return before === undefined || after === undefined ? [] : shrunk(`${url} lists`, before, after);
    }),
    ...shrunk("the sources list", committed.prefixes, refreshed.prefixes),
  ];
}

export async function download(url: string): Promise<Download> {
  try {
    const response = await fetch(url, { redirect: "error", signal: AbortSignal.timeout(30_000) });
    if (!response.ok) {
      return { failure: `HTTP ${response.status}` };
    }
    return { document: await response.json() };
  } catch (error) {
    return { failure: message(error) };
  }
}

export function warning(text: string): string {
  return `::warning title=Crawler ranges::${text.replaceAll("%", "%25").replaceAll("\r", "%0D").replaceAll("\n", "%0A")}`;
}

if (import.meta.main) {
  const { values } = parseArgs({ options: { "accept-shrink": { type: "boolean", default: false } } });
  const committed = readCrawlerList(readFileSync(listPath, "utf8"));
  const downloads = await Promise.all(sources.map((source) => download(source.url)));
  const dropped: string[] = [];
  const { list, kept } = crawlerList(downloads, committed, dropped);
  for (const prefix of dropped) {
    console.warn(`left out ${prefix}, which lies inside a block left out on purpose`);
  }
  const shrinks = shrinkage(committed, list);
  if (shrinks.length > 0 && !values["accept-shrink"]) {
    throw new Error(`${shrinks.join("; ")}. Compare them with the published lists; --accept-shrink proposes them anyway`);
  }
  for (const finding of shrinks) {
    console.log(warning(`accepted: ${finding}`));
  }
  for (const { url, failure } of kept) {
    console.log(warning(`${url} did not download (${failure}), so its committed ranges stay`));
  }
  writeFileSync(listPath, `${JSON.stringify(list, null, 2)}\n`);
  const output = process.env["GITHUB_OUTPUT"];
  if (output !== undefined) {
    appendFileSync(output, `kept=${kept.map(({ url }) => url).join(" ")}\n`);
  }
}
