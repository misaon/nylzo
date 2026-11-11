import { YAML } from "bun";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("release builds source without promoting cached CI artifacts", () => {
  const release = readFileSync(".github/workflows/release.yml", "utf8");
  assert(!release.includes("actions/cache"));
  assert(!release.includes("actions/go-cache"));
  assert(!release.includes("pattern: hashes-"));
  assert(!release.includes("name: web-dist"));
  assert(release.includes("bun run --cwd web --filter @nylzo/web build"));
  assert(!/^\s+cache: (?:pnpm|true|auto)$/mu.test(release));
  const toolSetups = release.match(/uses: \$\/\.github\/actions\/setup\n(?:\s+with:\n(?:\s{10}.*\n)*)?/gu) ?? [];
  assert(toolSetups.length > 0 && toolSetups.every(setup => setup.includes("cache: none")));
  assert(!release.includes("actions/setup-node@"));
  assert(!release.includes("pnpm/action-setup@"));
});

type Step = { name?: string; uses?: string; run?: string; with?: Record<string, string> };
test("the generated build config disables SCM publishing and keeps each image in its own loopback repository", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-release-config-"));
  try {
    const script = readFileSync("mise-tasks/release/build", "utf8");
    const preparation = script.slice(0, script.indexOf("goreleaser release"));
    const generated = spawnSync("bash", ["-c", preparation], {
      env: { ...process.env, RUNNER_TEMP: directory }, encoding: "utf8",
    });
    assert.equal(generated.status, 0, generated.stderr);
    const original = YAML.parse(readFileSync(".goreleaser.yaml", "utf8"));
    const config = YAML.parse(readFileSync(join(directory, "goreleaser-build.yaml"), "utf8"));
    const expected = structuredClone(original);
    expected.release.disable = true;
    for (const image of expected.kos) image.repositories = [`127.0.0.1:5000/${image.id}`];
    assert.deepEqual(config, expected);
    assert.equal(readFileSync(join(directory, "release-header.md"), "utf8").trim(), original.release.header.trim());
    assert.equal(readFileSync(join(directory, "release-footer.md"), "utf8").trim(), original.release.footer.trim());
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

const workflow = YAML.parse(readFileSync(".github/workflows/release.yml", "utf8")) as {
  permissions: Record<string, string>;
  jobs: Record<string, { permissions: Record<string, string>; needs: string[]; environment?: string; steps: Step[] }>;
};

test("native compilation and image builds have no publishing or OIDC permissions", () => {
  assert.deepEqual(workflow.permissions, {});
  assert.deepEqual(workflow.jobs.build.permissions, { contents: "read" });
  assert.equal(workflow.jobs.build.environment, undefined);
  assert.deepEqual(workflow.jobs.build.needs, ["guard", "web"]);
  const build = workflow.jobs.build.steps;
  assert(build.some(step => step.run === "bash mise-tasks/release/build --skip=before"));
  assert(!build.some(step => /login-action|attest-build-provenance/u.test(step.uses ?? "")));
  const script = readFileSync("mise-tasks/release/build", "utf8");
  assert(script.includes('.release.disable = true | .kos[] |= (.repositories = ["127.0.0.1:5000/" + .id])'));
  assert(script.includes("goreleaser release --clean --skip=sign"));
  assert(script.includes("--digest-tags --referrers"));
  assert(script.includes("ocidir://"));
});

test("the protected publisher verifies readonly artifacts and never rebuilds them", () => {
  const publisher = workflow.jobs.release;
  assert.deepEqual(publisher.needs, ["guard", "web", "build"]);
  assert.equal(publisher.environment, "${{ needs.guard.outputs.environment }}");
  const setup = publisher.steps.find(step => step.uses === "$/.github/actions/setup")!;
  assert.equal(setup.with?.tools, "regctl grype");
  const code = publisher.steps.map(step => step.run ?? step.uses ?? "").join("\n");
  assert(!/goreleaser|go build|bun |mise-tasks\/release\/build/u.test(code));
  const verify = code.indexOf('cmp - release-build.sha256');
  const publish = code.indexOf('regctl image copy ');
  assert(verify > 0 && verify < publish);
  assert(code.includes('test "$(regctl image digest "${image}:staging-${GITHUB_REF_NAME#v}")" = "$digest"'));
  assert(publisher.steps.some(step => step.with?.name === "release-build" && step.with?.["digest-mismatch"] === "error"));
});

test("the publisher rejects altered, missing, added or misidentified build artifacts", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-release-transfer-"));
  try {
    const artifact = join(directory, "release-build");
    const workspace = join(directory, "workspace");
    mkdirSync(join(artifact, "dist"), { recursive: true });
    mkdirSync(join(artifact, "images"));
    mkdirSync(workspace);
    const metadata = JSON.stringify({ tag: "v1.2.3", commit: "a".repeat(40) });
    writeFileSync(join(artifact, "dist/metadata.json"), metadata);
    writeFileSync(join(artifact, "images/image"), "original image");
    const generated = spawnSync("bash", ["-c", 'find . -type f -print0 | LC_ALL=C sort -z | xargs -0 shasum -a 256'], { cwd: artifact, encoding: "utf8" });
    assert.equal(generated.status, 0, generated.stderr);
    writeFileSync(join(artifact, "release-build.sha256"), generated.stdout);
    const hash = createHash("sha256").update(generated.stdout).digest("hex");
    const step = workflow.jobs.release.steps.find(step => step.name === "Verify the complete readonly build before using any artifact")!;
    // macOS has shasum; hosted Linux uses sha256sum. The manifest format and verifier are identical.
    const code = step.run!.replaceAll("sha256sum", "shasum -a 256");
    const verify = (extra: Record<string, string> = {}) => spawnSync("bash", ["-c", `set -euo pipefail\n${code}`], {
      env: { ...process.env, RUNNER_TEMP: directory, GITHUB_WORKSPACE: workspace, EXPECTED_MANIFEST_SHA256: hash, GITHUB_REF_NAME: "v1.2.3", GITHUB_SHA: "a".repeat(40), ...extra },
      encoding: "utf8",
    });
    assert.equal(verify().status, 0);
    rmSync(join(workspace, "dist"), { recursive: true });
    writeFileSync(join(artifact, "images/image"), "changed image");
    assert.notEqual(verify().status, 0);
    rmSync(join(artifact, "images/image"));
    assert.notEqual(verify().status, 0);
    writeFileSync(join(artifact, "images/image"), "original image");
    writeFileSync(join(artifact, "dist/unexpected"), "unlisted file");
    assert.notEqual(verify().status, 0);
    rmSync(join(artifact, "dist/unexpected"));
    assert.notEqual(verify({ EXPECTED_MANIFEST_SHA256: "0".repeat(64) }).status, 0);
    assert.notEqual(verify({ GITHUB_REF_NAME: "v9.9.9" }).status, 0);
    assert.notEqual(verify({ GITHUB_SHA: "b".repeat(40) }).status, 0);
    assert.equal(verify().status, 0);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
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
