import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { checkShrink, crawlerList, listPath, rangesOf, sources } from "./crawler-ranges.mts";

const list = (...prefixes: object[]) => ({ creationTime: "2026-11-08T00:00:00", prefixes });
const both = ["ipv4Prefix", "ipv6Prefix"] as const;
const googlebot = list({ ipv4Prefix: "66.249.64.0/27" });
const sourceIndex = (file: string) => sources.findIndex((source) => source.url.endsWith(`/${file}`));
const documents = (replaced: Record<number, object> = {}) => sources.map((_, index) => replaced[index] ?? googlebot);

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
  assert.throws(() => crawlerList(documents({ 1: list() })), { message: `${sources[1]!.url}: the list has no prefixes` });
});

test("the shared block named for ChatGPT-User is left out of every source whole or in pieces, any other wide range still stops the refresh", () => {
  const chatgptUser = sourceIndex("chatgpt-user.json");
  const shared = { ipv4Prefix: "9.129.0.0/16" };
  const fetcher = { ipv4Prefix: "20.0.0.0/28" };
  assert.deepEqual(crawlerList(documents({ [chatgptUser]: list(shared, fetcher) })).prefixes, ["20.0.0.0/28", "66.249.64.0/27"]);
  const pieces = list({ ipv4Prefix: "9.129.0.0/20" }, { ipv4Prefix: "9.129.240.0/28" }, fetcher);
  const dropped: string[] = [];
  assert.deepEqual(crawlerList(documents({ [chatgptUser]: pieces }), dropped).prefixes, ["20.0.0.0/28", "66.249.64.0/27"]);
  assert.deepEqual(dropped, ["9.129.0.0/20", "9.129.240.0/28"]);
  assert.throws(() => crawlerList(documents({ [chatgptUser]: list({ ipv4Prefix: "9.130.0.0/16" }, fetcher) })), /wider than \/20/u);
  assert.deepEqual(crawlerList(documents({ [sourceIndex("searchbot.json")]: list({ ipv4Prefix: "9.129.16.0/20" }, fetcher) })).prefixes, [
    "20.0.0.0/28",
    "66.249.64.0/27",
  ]);
});

test("a refresh stops when the sources together list far more addresses than today", () => {
  const wide = Array.from({ length: 33 }, (_, index) => ({ ipv4Prefix: `66.${200 + Math.floor(index / 16)}.${(index % 16) * 16}.0/20` }));
  assert.throws(() => crawlerList(documents({ 0: list(...wide) })), /more than the ceiling/u);
  const many = Array.from({ length: 8001 }, (_, index) => ({ ipv6Prefix: `2a00:1450:${index.toString(16)}::/48` }));
  assert.throws(() => crawlerList(documents({ 0: list(...many) })), /8002 prefixes .* more than the ceiling/u);
});

test("a refresh stops when the sources together list far fewer prefixes or addresses than the committed list", () => {
  const committed = ["66.249.64.0/27", "66.249.65.0/27", "157.55.39.0/24", "2001:4860:4801:10::/64"];
  checkShrink(committed, ["66.249.64.0/27", "66.249.66.0/27", "157.55.39.0/24", "2001:4860:4801:10::/64"]);
  assert.throws(() => checkShrink(committed, ["66.249.64.0/27", "157.55.39.0/24"]), /far fewer/u);
  assert.throws(() => checkShrink(committed, ["66.249.64.0/27", "66.249.65.0/27", "66.249.66.0/27", "2001:4860:4801:10::/64"]), /far fewer/u);
});

test("Bing contributes only IPv4 and the list is sorted without duplicates", () => {
  const result = crawlerList(
    documents({
      0: list({ ipv6Prefix: "2001:4860:4801:10::/64" }, { ipv4Prefix: "66.249.64.0/27" }),
      2: list({ ipv4Prefix: "66.249.64.0/27" }, { ipv6Prefix: "2001:4860:4801:4::/64" }),
      [sourceIndex("bingbot.json")]: list({ ipv4Prefix: "157.55.39.0/24" }, { ipv6Prefix: "2620:1ec:c::/48" }),
      [sourceIndex("seznambot.json")]: list({ ipv4Prefix: "77.75.72.26/32" }),
    }),
  );
  assert.deepEqual(result, {
    sources: sources.map((source) => source.url),
    prefixes: ["66.249.64.0/27", "77.75.72.26/32", "157.55.39.0/24", "2001:4860:4801:4::/64", "2001:4860:4801:10::/64"],
  });
});

test("the committed list is what the generator writes", () => {
  const committed: unknown = JSON.parse(readFileSync(listPath, "utf8"));
  assert.deepEqual(Object.keys(committed as object), ["sources", "prefixes"]);
  assert.deepEqual(Reflect.get(committed as object, "sources"), sources.map((source) => source.url));
  const prefixes = Reflect.get(committed as object, "prefixes") as string[];
  const entries = prefixes.map((prefix) => (prefix.includes(":") ? { ipv6Prefix: prefix } : { ipv4Prefix: prefix }));
  assert.deepEqual(crawlerList(sources.map(() => list(...entries))).prefixes, prefixes);
});
