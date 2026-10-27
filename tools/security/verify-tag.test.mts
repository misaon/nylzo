import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("manual and keyless release identities cannot be substituted or replayed under another tag", () => {
  const directory = mkdtempSync(join(tmpdir(), "nylzo-tag-guard-"));
  try {
    const commands = {
      git: `#!/bin/sh
case "$1" in
 fetch) exit 0 ;;
 cat-file) if [ "$2" = -t ]; then echo tag; else printf 'object aaaa\\ntype commit\\ntag %s\\ntagger test\\n\\nsignature\\n' "$EMBEDDED_TAG"; fi ;;
 rev-parse) echo "$GITHUB_SHA" ;;
 merge-base) exit "${"${ANCESTOR_STATUS:-0}"}" ;;
esac
`,
      sleep: '#!/bin/sh\nexit 0\n',
      gh: '#!/bin/sh\nprintf "%s" "$TAG_JSON"\n',
      gitsign: '#!/bin/sh\nprintf "%s\\n" "$@" > "$RUNNER_TEMP/claims"\nexit "${SIGNATURE_STATUS:-0}"\n',
    };
    for (const [name, source] of Object.entries(commands)) { const path = join(directory, name); writeFileSync(path, source); chmodSync(path, 0o700); }
    const run = (overrides: Record<string,string> = {}) => {
      const output = join(directory, "output"); writeFileSync(output, "");
      const result = spawnSync("bash", ["tools/security/verify-tag"], { encoding: "utf8", env: {
        ...process.env, PATH: `${directory}:${process.env.PATH}`, RUNNER_TEMP: directory, GITHUB_OUTPUT: output,
        GITHUB_REPOSITORY: "misaon/nylzo", GITHUB_REF_NAME: "v0.1.2", EMBEDDED_TAG: "v0.1.2",
        GITHUB_SHA: "a".repeat(40), GITHUB_ACTOR: "nylzo-security[bot]", RELEASE_TAGGER_EMAIL: "owner@example.com",
        SECURITY_AUTOMATION_ENABLED: "true", SECURITY_APP_SLUG: "nylzo-security",
        TAG_JSON: JSON.stringify({ verification: { verified: true }, tagger: { email: "owner@example.com" } }), ...overrides,
      }});
      return { ...result, output: readFileSync(output, "utf8") };
    };
    assert.equal(run().output, "environment=release\n");
    const unsigned = JSON.stringify({ verification: { verified: false }, tagger: { email: "automation@nylzo.com" } });
    assert.equal(run({ TAG_JSON: unsigned }).output, "environment=release-automatic\n");
    const claims = readFileSync(join(directory, "claims"), "utf8");
    assert(claims.startsWith("verify-tag\n"));
    assert(claims.endsWith("v0.1.2\n"));
    assert(claims.includes("https://github.com/misaon/nylzo/.github/workflows/security-updates.yml@refs/heads/main"));
    assert(claims.includes("--certificate-github-workflow-sha\n" + "a".repeat(40)));
    assert(claims.includes("--certificate-github-workflow-trigger\nworkflow_run"));
    for (const overrides of [
      { EMBEDDED_TAG: "automation-probe-123" }, { GITHUB_REPOSITORY: "attacker/nylzo" },
      { GITHUB_REF_NAME: "v0.01.2" }, { RELEASE_TAGGER_EMAIL: "someone-else@example.com" },
      { ANCESTOR_STATUS: "1" },
      { TAG_JSON: unsigned, SECURITY_AUTOMATION_ENABLED: "false" },
      { TAG_JSON: unsigned, GITHUB_ACTOR: "attacker" },
      { TAG_JSON: unsigned, SIGNATURE_STATUS: "1" },
    ]) assert.notEqual(run(overrides).status, 0);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
