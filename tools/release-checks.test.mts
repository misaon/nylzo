import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("publication rebuilds source without promoting cached CI artifacts", () => {
  const release = readFileSync(".github/workflows/release.yml", "utf8");
  assert(!release.includes("actions/cache/"));
  assert(!release.includes("pattern: hashes-"));
  assert(!release.includes("name: web-dist"));
  assert(release.includes("bun run --cwd web --filter @nylzo/web build"));
  assert(!/^\s+cache: (?:pnpm|true|auto)$/mu.test(release));
  const toolSetups = release.match(/uses: \$\/\.github\/actions\/mise\n(?:\s+with:\n(?:\s{10}.*\n)*)?/gu) ?? [];
  assert(toolSetups.length > 0 && toolSetups.every(setup => setup.includes("cache: none")));
  assert(!release.includes("actions/setup-node@"));
  assert(!release.includes("pnpm/action-setup@"));
});

test("the in-toto provenance Scorecard reads is verified and attached before the immutable release is published", () => {
  const release = readFileSync(".github/workflows/release.yml", "utf8");
  const verified = release.indexOf('gh attestation verify "$artifact" --repo "$GITHUB_REPOSITORY" --bundle "$provenance"');
  const attached = release.indexOf('gh release upload "$GITHUB_REF_NAME" --repo "$GITHUB_REPOSITORY" "$provenance"');
  const published = release.indexOf("--draft=false");
  assert.ok(0 < verified && verified < attached && attached < published);
  assert.match(release, /^\s+provenance="\$RUNNER_TEMP\/nylzo_\$\{version\}\.intoto\.jsonl"$/mu);
  assert.match(readFileSync(".goreleaser.yaml", "utf8"), /^ {2}draft: true$/mu);
});

test("a release requires full verification and successful security checks from the same run", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-release-checks-"));
  try {
    const executable = join(directory, "gh");
    writeFileSync(executable, `#!/bin/sh
printf '%s\\n' "$CHECKS"
for argument; do
  if [ "$argument" = --paginate ]; then
    printf '%s\\n' "$LATER_PAGES"
  fi
done
exit "\${GH_EXIT_STATUS:-0}"
`);
    chmodSync(executable, 0o700);
    const names = [
      "Release verification",
      "CI required",
      "Analyze (go)",
      "Analyze (javascript-typescript)",
      "Analyze (actions)",
    ];
    const checks = names.map((name) => ({
      name,
      app: { slug: "github-actions" },
      conclusion: "success",
      check_suite: { id: 1 },
    }));
    const verify = (checkRuns: typeof checks, laterPages: (typeof checks)[] = [], exitStatus = 0) =>
      spawnSync("bash", ["mise-tasks/release/checks", "a".repeat(40)], {
        env: {
          ...process.env,
          PATH: `${directory}:${process.env.PATH}`,
          CHECKS: JSON.stringify({ check_runs: checkRuns }),
          LATER_PAGES: laterPages.map(page => JSON.stringify({ check_runs: page })).join("\n"),
          GH_EXIT_STATUS: String(exitStatus),
        },
        encoding: "utf8",
      });
    assert.equal(verify(checks).status, 0);
    const unrelated = Array.from({ length: 100 }, (_, index) => ({
      ...checks[0],
      name: `Unrelated check ${index}`,
    }));
    assert.notEqual(verify(unrelated).status, 0);
    assert.equal(verify(unrelated, [checks.slice(0, 2), checks.slice(2)]).status, 0);
    assert.notEqual(verify(unrelated, [checks.slice(0, -1)]).status, 0);
    assert.notEqual(verify(unrelated, [checks.slice(0, 2), checks.slice(2)], 1).status, 0);
    for (let index = 0; index < checks.length; index++) {
      assert.notEqual(verify(checks.filter((_, position) => position !== index)).status, 0);
      for (const conclusion of ["skipped", "cancelled", "failure"]) {
        assert.notEqual(
          verify(unrelated, [
            checks.map((check, position) => (position === index ? { ...check, conclusion } : check)),
          ]).status,
          0,
        );
        assert.notEqual(
          verify(checks.map((check, position) => (position === index ? { ...check, conclusion } : check))).status,
          0,
        );
      }
      assert.notEqual(
        verify(unrelated, [
          checks.map((check, position) => (position === index ? { ...check, check_suite: { id: 2 } } : check)),
        ]).status,
        0,
      );
      assert.notEqual(
        verify(checks.map((check, position) => (position === index ? { ...check, check_suite: { id: 2 } } : check)))
          .status,
        0,
      );
      assert.notEqual(
        verify(
          checks.map((check, position) => (position === index ? { ...check, app: { slug: "untrusted-app" } } : check)),
        ).status,
        0,
      );
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("Caddy runs under its own UID and GID, apart from Nylzo's distroless 65532", () => {
  const services = readFileSync("deploy/compose/compose.yaml", "utf8").split(/^ {2}(?=[a-z-]+:\n)/mu);
  const user = (name: string) => /^ {4}user: "(\d+):(\d+)"$/mu.exec(services.find(block => block.startsWith(`${name}:\n`)) ?? "")?.slice(1);
  assert.equal(user("nylzo"), undefined, "Nylzo keeps the distroless nonroot user, UID 65532");
  assert.deepEqual(user("caddy"), ["65533", "65533"]);
  const images = readFileSync(".goreleaser.yaml", "utf8").split(/^ {2}- id: /mu);
  const image = (repository: string) => images.find(block => block.includes(`repositories: [${repository}]\n`)) ?? "";
  assert.doesNotMatch(image("ghcr.io/misaon/nylzo"), /^ {4}user:/mu);
  assert.match(image("ghcr.io/misaon/nylzo-caddy"), /^ {4}user: "65533:65533"$/mu);
});
