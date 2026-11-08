import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { crawlerList, listPath, rangesOf, sources } from "./crawler-ranges.mts";

const list = (...prefixes: object[]) => ({ creationTime: "2026-11-08T00:00:00", prefixes });
const both = ["ipv4Prefix", "ipv6Prefix"] as const;

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
    [{ ipv4Prefix: "66.249.66.0/24/1" }, /not an IPv4 prefix/u],
    [{ ipv6Prefix: "2001:4860:4801:10::1::/64" }, /not an IPv6 prefix/u],
    [{ ipv4Prefix: "66.249.66.0/24", ipv6Prefix: "2001:4860:4801:10::/64" }, /unexpected entry/u],
  ] as const) {
    assert.throws(() => rangesOf(list(entry), both), reason, JSON.stringify(entry));
  }
  assert.throws(() => rangesOf({ prefixes: [] }, both), /no prefixes/u);
  assert.throws(() => rangesOf(list({ ipv6Prefix: "2001:4860:4801:10::/64" }), ["ipv4Prefix"]), /no usable prefixes/u);
  assert.throws(
    () => crawlerList([list({ ipv4Prefix: "66.249.64.0/27" }), list(), list({ ipv4Prefix: "157.55.39.0/24" }), list({ ipv4Prefix: "77.75.72.26/32" })]),
    new RegExp(`${sources[1]!.url.replaceAll(".", "\\.")}: the list has no prefixes$`, "u"),
  );
});

test("Bing contributes only IPv4 and the list is sorted without duplicates", () => {
  const result = crawlerList([
    list({ ipv6Prefix: "2001:4860:4801:10::/64" }, { ipv4Prefix: "66.249.64.0/27" }),
    list({ ipv4Prefix: "66.249.64.0/27" }, { ipv6Prefix: "2001:4860:4801:4::/64" }),
    list({ ipv4Prefix: "157.55.39.0/24" }, { ipv6Prefix: "2620:1ec:c::/48" }),
    list({ ipv4Prefix: "77.75.72.26/32" }),
  ]);
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
