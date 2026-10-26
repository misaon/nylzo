import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { normalizeReport } from "./normalize.mts";

test("monorepo reports retain their source identity and cannot escape the selected project", () => {
  for (const project of ["web/core", "web/app"]) {
    const source = "src/encoding.ts";
    for (const filename of [source, path.resolve(project, source)]) {
      assert.equal(
        normalizeReport(`TN:\nSF:${filename}\nDA:1,2\nend_of_record\n`, project),
        `TN:\nSF:${project}/${source}\nDA:1,2\nend_of_record\n`,
      );
    }
    assert.throws(() => normalizeReport("SF:../../README.md\n", project));
    assert.throws(() => normalizeReport("SF:../other/src/file.ts\n", project));
  }
  assert.throws(() => normalizeReport("SF:src/file.ts\n", "unknown"));
});
