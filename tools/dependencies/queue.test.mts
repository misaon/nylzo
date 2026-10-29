import assert from "node:assert/strict";
import test from "node:test";
import { blockedProposals } from "./queue.mts";

test("obsolete release holds cannot inflate the active proposal budget", () => {
  const saved = { ...process.env };
  try {
    process.env.DEPENDENCY_APP_SLUG = "nylzo-dependency-updates";
    process.env.DEPENDENCY_BOT_ID = "123456";
    const pull = (base: string, id = 123456) => ({ user: { login: "nylzo-dependency-updates[bot]", id },
      head: { repo: { full_name: "misaon/nylzo" } }, base: { ref: base }, labels: [{ name: "requires-attention" }] });
    const pulls = [pull("main"), pull("release/v0.1.10"), ...Array.from({ length: 20 }, () => pull("release/v0.1.9")), pull("main", 999)];
    assert.equal(blockedProposals(pulls, "v0.1.10"), 2);
    assert.equal(blockedProposals(pulls, "v0.1.11"), 1);
    pulls[0]!.labels = [];
    assert.equal(blockedProposals(pulls, "v0.1.10"), 1);
  } finally { process.env = saved; }
});
