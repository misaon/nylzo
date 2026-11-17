import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import test from "node:test";

import type { CrawlerList, Download } from "./crawler-ranges.mts";
import { crawlerList, download, listPath, rangesOf, readCrawlerList, shrinkage, sources, warning } from "./crawler-ranges.mts";

const list = (...prefixes: object[]) => ({ creationTime: "2026-11-08T00:00:00", prefixes });
const both = ["ipv4Prefix", "ipv6Prefix"] as const;
const googlebot = list({ ipv4Prefix: "66.249.64.0/27" });
const sourceIndex = (file: string) => sources.findIndex((source) => source.url.endsWith(`/${file}`));
const documents = (replaced: Record<number, object> = {}): Download[] => sources.map((_, index) => ({ document: replaced[index] ?? googlebot }));
const nothingCommitted: CrawlerList = { sources: {}, prefixes: [] };
const prefixesOf = (replaced: Record<number, object>) => crawlerList(documents(replaced), nothingCommitted).list.prefixes;
const committedList = (ranges: Record<string, string[]>): CrawlerList => ({
  sources: ranges,
  prefixes: [...new Set(Object.values(ranges).flat())],
});

test("a refresh never widens the list beyond published public crawler ranges", () => {
  for (const [entry, reason] of [
    [{ ipv4Prefix: "66.249.0.0/16" }, /wider than \/20/u],
    [{ ipv6Prefix: "2001:4860::/32" }, /wider than \/48/u],
    [{ ipv4Prefix: "0.0.0.0/0" }, /wider than/u],
    [{ ipv4Prefix: "66.249.66.1/27" }, /not a network address/u],
    [{ ipv4Prefix: "10.1.0.0/24" }, /not a public address range/u],
    [{ ipv4Prefix: "127.0.0.0/24" }, /not a public address range/u],
    [{ ipv6Prefix: "fd00::/64" }, /not a public address range/u],
    [{ ipv6Prefix: "2001:db8:1::/64" }, /not a public address range/u],
    [{ ipv6Prefix: "2001:0:4136::/48" }, /not a public address range/u],
    [{ ipv6Prefix: "2002:c000:204::/48" }, /not a public address range/u],
    [{ ipv4Prefix: "66.249.66.0/24/1" }, /not an IPv4 prefix/u],
    [{ ipv6Prefix: "2001:4860:4801:10::1::/64" }, /not an IPv6 prefix/u],
    [{ ipv4Prefix: "66.249.66.0/24", ipv6Prefix: "2001:4860:4801:10::/64" }, /unexpected entry/u],
  ] as const) {
    assert.throws(() => rangesOf(list(entry), both), reason, JSON.stringify(entry));
  }
  assert.throws(() => rangesOf({ prefixes: [] }, both), /no prefixes/u);
  assert.throws(() => rangesOf(list({ ipv6Prefix: "2001:4860:4801:10::/64" }), ["ipv4Prefix"]), /no usable prefixes/u);
  assert.throws(() => prefixesOf({ 1: list() }), { message: `${sources[1]!.url}: the list has no prefixes` });
});

test("the shared block named for ChatGPT-User is left out of every source whole or in pieces, any other wide range still stops the refresh", () => {
  const chatgptUser = sourceIndex("chatgpt-user.json");
  const shared = { ipv4Prefix: "9.129.0.0/16" };
  const fetcher = { ipv4Prefix: "20.0.0.0/28" };
  assert.deepEqual(prefixesOf({ [chatgptUser]: list(shared, fetcher) }), ["20.0.0.0/28", "66.249.64.0/27"]);
  const pieces = list({ ipv4Prefix: "9.129.0.0/20" }, { ipv4Prefix: "9.129.240.0/28" }, fetcher);
  const dropped: string[] = [];
  assert.deepEqual(crawlerList(documents({ [chatgptUser]: pieces }), nothingCommitted, dropped).list.prefixes, ["20.0.0.0/28", "66.249.64.0/27"]);
  assert.deepEqual(dropped, ["9.129.0.0/20", "9.129.240.0/28"]);
  assert.throws(() => prefixesOf({ [chatgptUser]: list({ ipv4Prefix: "9.130.0.0/16" }, fetcher) }), /wider than \/20/u);
  assert.deepEqual(prefixesOf({ [sourceIndex("searchbot.json")]: list({ ipv4Prefix: "9.129.16.0/20" }, fetcher) }), ["20.0.0.0/28", "66.249.64.0/27"]);
});

test("a refresh stops when the sources together list far more addresses than today", () => {
  const wide = Array.from({ length: 33 }, (_, index) => ({ ipv4Prefix: `66.${200 + Math.floor(index / 16)}.${(index % 16) * 16}.0/20` }));
  assert.throws(() => prefixesOf({ 0: list(...wide) }), /more than the ceiling/u);
  const many = Array.from({ length: 8001 }, (_, index) => ({ ipv6Prefix: `2a00:1450:${index.toString(16)}::/48` }));
  assert.throws(() => prefixesOf({ 0: list(...many) }), /8002 prefixes .* more than the ceiling/u);
});

test("a source that lists far fewer prefixes or addresses than its committed ranges stops the refresh, even when the total barely moves", () => {
  const bing = sources[sourceIndex("bingbot.json")]!.url;
  const mistral = sources[sourceIndex("mistralai-index-ips.json")]!.url;
  const google = Array.from({ length: 40 }, (_, index) => `66.249.${index}.0/27`);
  const committed = committedList({
    [sources[0]!.url]: google,
    [bing]: ["157.55.39.0/24", "207.46.13.0/24"],
    [mistral]: ["2001:4860:4801:10::/64", "2001:4860:4801:11::/64", "2001:4860:4801:12::/64", "2001:4860:4801:13::/64"],
  });
  const refreshed = (ranges: Record<string, string[]>) => committedList({ ...committed.sources, ...ranges });
  assert.deepEqual(shrinkage(committed, refreshed({ [bing]: ["157.55.39.0/24", "207.46.13.0/24"] })), []);
  assert.deepEqual(shrinkage(committed, refreshed({ [bing]: ["157.55.39.0/24", "207.46.13.0/26"] })), [
    `${bing} lists 2 prefixes and 320 IPv4 addresses, far fewer than the 2 and 512 of the committed list`,
  ]);
  assert.deepEqual(shrinkage(committed, refreshed({ [mistral]: ["2001:4860:4801:10::/64", "2001:4860:4801:11::/64"] })), [
    `${mistral} lists 2 prefixes and 0 IPv4 addresses, far fewer than the 4 and 0 of the committed list`,
  ]);
  const total = shrinkage(committed, refreshed({ [sources[0]!.url]: google.slice(0, 20) }));
  assert.equal(total.length, 2);
  assert.match(total[1]!, /^the sources list 26 prefixes .* far fewer than the 46 /u);
});

test("a source that does not download keeps its committed ranges and is named, the others still refresh", () => {
  const bing = sourceIndex("bingbot.json");
  const committed = committedList({ [sources[bing]!.url]: ["157.55.39.0/24"] });
  const downloads = documents({ 0: list({ ipv4Prefix: "66.249.66.0/27" }) }).with(bing, { failure: "HTTP 403" });
  const { list: refreshed, kept } = crawlerList(downloads, committed);
  assert.deepEqual(kept, [{ url: sources[bing]!.url, failure: "HTTP 403" }]);
  assert.deepEqual(refreshed.sources[sources[bing]!.url], ["157.55.39.0/24"]);
  assert.deepEqual(refreshed.prefixes, ["66.249.64.0/27", "66.249.66.0/27", "157.55.39.0/24"]);
  assert.deepEqual(shrinkage(committed, refreshed), []);
  const seznam = sourceIndex("seznambot.json");
  assert.throws(() => crawlerList(downloads.with(seznam, { failure: "HTTP 503" }), committed), {
    message: `${sources[seznam]!.url}: HTTP 503, and the committed list holds no ranges of it to keep`,
  });
});

test("an HTTP error, a redirect or a page that is not JSON is a download failure, not a stop", async (context) => {
  const server = createServer((request, response) => {
    if (request.url === "/moved") {
      response.writeHead(302, { location: "/ranges" }).end();
    } else if (request.url === "/challenge") {
      response.writeHead(200, { "content-type": "text/html" }).end("<html>Just a moment...</html>");
    } else if (request.url === "/ranges") {
      response.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify(googlebot));
    } else {
      response.writeHead(403).end();
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  context.after(() => server.close());
  const address = server.address();
  const origin = typeof address === "object" && address !== null ? `http://127.0.0.1:${address.port}` : "";
  assert.deepEqual(await download(`${origin}/ranges`), { document: googlebot });
  assert.deepEqual(await download(`${origin}/blocked`), { failure: "HTTP 403" });
  for (const path of ["/moved", "/challenge"]) {
    const result = await download(`${origin}${path}`);
    assert.ok("failure" in result && result.failure !== "", path);
  }
});

test("a warning keeps a downloaded error message on one annotation line", () => {
  assert.equal(warning("a 100% failure\n::error::x\r"), "::warning title=Crawler ranges::a 100%25 failure%0A::error::x%0D");
});

test("Bing contributes only IPv4 and every list is sorted without duplicates", () => {
  const { list: result } = crawlerList(
    documents({
      0: list({ ipv6Prefix: "2001:4860:4801:10::/64" }, { ipv4Prefix: "66.249.64.0/27" }),
      2: list({ ipv4Prefix: "66.249.64.0/27" }, { ipv6Prefix: "2001:4860:4801:4::/64" }),
      [sourceIndex("bingbot.json")]: list({ ipv4Prefix: "157.55.39.0/24" }, { ipv6Prefix: "2620:1ec:c::/48" }),
      [sourceIndex("seznambot.json")]: list({ ipv4Prefix: "77.75.72.26/32" }, { ipv4Prefix: "77.75.72.26/32" }),
    }),
    nothingCommitted,
  );
  assert.deepEqual(result.prefixes, ["66.249.64.0/27", "77.75.72.26/32", "157.55.39.0/24", "2001:4860:4801:4::/64", "2001:4860:4801:10::/64"]);
  assert.deepEqual(result.sources[sources[0]!.url], ["66.249.64.0/27", "2001:4860:4801:10::/64"]);
  assert.deepEqual(result.sources[sources[sourceIndex("bingbot.json")]!.url], ["157.55.39.0/24"]);
  assert.deepEqual(result.sources[sources[sourceIndex("seznambot.json")]!.url], ["77.75.72.26/32"]);
});

test("the committed list is what the generator writes, and every source can fall back on it", () => {
  const text = readFileSync(listPath, "utf8");
  const committed = readCrawlerList(text);
  assert.deepEqual(Object.keys(JSON.parse(text) as object), ["sources", "prefixes"]);
  assert.deepEqual(Object.keys(committed.sources), sources.map((source) => source.url));
  const failed = sources.map(() => ({ failure: "HTTP 503" }));
  const { list: refreshed, kept } = crawlerList(failed, committed);
  assert.equal(`${JSON.stringify(refreshed, null, 2)}\n`, text);
  assert.equal(kept.length, sources.length);
  assert.throws(() => readCrawlerList(JSON.stringify({ sources: sources.map((source) => source.url), prefixes: committed.prefixes })), /ranges of each source/u);
});
